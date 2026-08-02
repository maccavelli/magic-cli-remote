# MADR 0062 — Implementation plan: phone transport selection

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to
[0062-MADR-phone-transport-selection.md](0062-MADR-phone-transport-selection.md).
This is the **review and build plan**: it re-grounds the MADR against the
current Flutter tree, names concrete APIs/files/tests, sequences work so policy
ships before UI, and defines acceptance gates.

- **Status:** **Implemented 2026-08-01** — P0 → P4 landed, one commit per phase.
  Amendments A1–A5 are folded back into 0062-MADR (D4/D7/D8/D10/D11); the
  supersession note is in 0061-MADR. Automated gates G1–G6 and G9–G11 are
  green; **G7 (the §6.4 hybrid-hardware checklist) is outstanding** and is the
  remaining sign-off before ship.
- **Date:** 2026-08-01
- **Scope:** Flutter mobile app only (`apps/mobile`). No mcremote/mcrelay
  protocol or pair URI wire changes.
- **MADR lock-ins (must not regress):** D1–D11 including Socratic amendments
  (DialEpisode, D10 taxonomy, QR defer when dual-available, network-generation
  fallback budget, Settings forced primary).
- **MADR amendments this plan proposes (see §10):** A1 claim-fallback latch
  (D4/D10), A2 config-error hop (D10), A3 nullable `transport` (D7), A4 single
  sticky value in v1 (D8), A5 user-initiated episodes exempt from the
  generation budget (D11). These must be reflected back into 0062-MADR on
  approval — do **not** treat the plan as silently overriding a locked D.
- **Standards:** project `AGENTS.md`; Dart format (`dart format`);
  `flutter analyze` + targeted `flutter test` before commit; mobile tests under
  `apps/mobile/test/`.

---

## 0. Assessment vs codebase (grounding)

### 0.1 What already exists (reuse)

| Capability | Location | Notes |
|------------|----------|-------|
| Direct dial | `McremoteClient._openSocketDirect` | 8s ready timeout |
| Relay dial | `_openSocketViaRelay` + `RelayTransport.open` | Outer join + loopback factory; 20s ready |
| Mesh soft probe | `McremoteClient.probeDirectReachable` | ~900ms; healthz → TLS → TCP; `badCertificateCallback` true |
| Relay base URL helper | `RelayTransport.phoneWsUrl` | Normalize to `wss://…/v1/phone` |
| Relay healthz (server) | mcrelay `GET /healthz` → `{"ok":true}` | Soft edge-up only |
| Pair parse | `PairPayload` (`hasRelay`, `relay`, `hostId`) | Both or neither |
| Path helper (status) | `ConnectionPath.resolve` | Not used for dial today |
| Attempt/store relay | ConnectScreen + `SettingsStore.setRelayRoute` | Authority-scoped |
| Connect epoch | `_connectEpoch`, `_staleAttempt` | Supersede in-flight connect |
| Reconnect gate | `_reconnectInFlight`, `_scheduleReconnect` | Timer backoff; **no** transport mode |
| Permanent auth set | `mc_exception.dart` `_permanentAuthCodes` / pair set | Overlaps D10; must **align** DialEpisode taxonomy |
| Lifecycle triggers | `app_lifecycle.dart` | Resume + connectivity → `reconnectFromStore` |
| Settings route status | `settings_screen.dart` “Route” ListTile | Display only |
| Tests | `relay_path_test`, `relay_transport_test`, `connect_screen_test`, `mcremote_client_test`, `lifecycle_policy_test` | Extend; do not rewrite wholesale |

### 0.2 Gaps the MADR requires (not present today)

| Gap | Current behaviour | Required |
|-----|-------------------|----------|
| Explicit `TransportMode` | `_shouldUseRelay` boolean heuristics | Mode on every dial leg |
| Dual probes for UI | Mesh probe only inside path choice | Parallel mesh + relay healthz → availability |
| Menu only if both available | N/A | Strict dual-pass |
| QR dual-available | `_applyPair` **immediately** claims/connects | Populate → probe → **wait** for Connect if dual |
| Unified failover | None on interactive; reconnect has no transport hop | **DialEpisode** one alternate on retryable fail |
| Sticky last success | Not stored | Per-authority prefs |
| Settings transport control | Status only | Selector + Reconnect now (forced primary) |
| Network generation | N/A | Bump on connectivity/resume; fallback budget per gen |
| Permanent vs transport-retryable | `McException.permanent` for **auto-reconnect stop** | Separate set for **transport fallback** (D10) |

### 0.3 Critical design risks (from Socratic + code)

1. **Probe ≠ join:** relay `/healthz` can pass while `host_offline` on join.
   Mitigation: DialEpisode fallback + Try-other; do not ban sticky on probe fail.
2. **Mesh false-positive:** half-up Tailscale can pass healthz/TCP.
   Mitigation: same DialEpisode; improve probe later without API change.
3. **QR auto-start vs menu:** must change `_applyPair` control flow or dual
   menu is dead code.
4. **Reconnect thrash:** lifecycle + timer + Settings can stack episodes.
   Mitigation: D11 — single in-flight episode + generation-scoped fallback.
5. **Taxonomy collision:** `permanent` on `McException` stops **background
   reconnect loops** (0046). Transport fallback must use D10 **or** a new
   `transportRetryable` helper that does not weaken 0046 permanent-auth stop.
6. **Claim before token — pair codes are one-shot (BLOCKING).** The daemon
   consumes the code in `PairCodeStore.Take` (`internal/auth/paircode.go:190`)
   and only puts it back via `Restore` when the subsequent device-create
   fails. So once the claim frame is on the wire, the code may already be
   spent even if the phone never sees `pair_ok`. A naive "auth_timeout is
   retryable" hop therefore burns the pairing:
   mesh socket opens → claim sent → daemon takes code + mints token → phone
   times out (`mcremote_client.dart:1047`) → episode hops to relay → resends
   the same code → `invalid_code` (permanent). The user is stranded holding a
   token that exists on the host and never arrived.
   **Mitigation (amendment A1):** DialEpisode carries a `claimSent` latch; for
   the `claimPairCode` entry, automatic fallback is allowed on **pre-claim**
   failures only. See §1.3 and §1.4. Token connect is idempotent and keeps the
   full retryable set.
7. **Cold auto-connect** (`ConnectScreen._load`) must use DialEpisode with
   sticky primary, not raw `connect` without mode.
8. **Config errors must not strand a working transport (BLOCKING).**
   `_openSocketViaRelay` throws `relay_misconfigured` (`mcremote_client.dart:544`)
   whenever the tuple is missing — including the ordinary case where sticky
   says relay but `setRelayRoute` cleared the route on an authority change. It
   is thrown `permanent: true`, so under a naive D10 reading it also parks
   background reconnect (0046). Treating it as fallback-forbidding strands a
   perfectly reachable mesh. **Mitigation (amendment A2):** see §1.4.
9. **Episode wall-clock (BLOCKING for UX):** mesh `ready` is 8s
   (`_openSocketDirect`) and relay `ready` is 20s (`_openSocketViaRelay`), so
   a mesh→relay episode can sit at "Connecting…" for ~28s plus ~1s of probes
   with no feedback. The episode needs progress reporting and a cap (§1.3).
10. **P0.5 sequencing regression:** removing attempt-force-relay (G2) before
    the probes land makes off-mesh QR pairing *slower* than today. See §4 P0.5.

### 0.4 Out of scope (do not implement in this plan)

- Pair URI `transport=` / `mesh=0` flags
- mcrelay/mcremote server changes
- Multi-hop failover chains
- Changing Host field to show relay URL
- Restoring 0061 force-relay-on-QR

---

## 1. Target architecture

```text
                    ┌─────────────────────────────┐
                    │  TransportPolicy (pure)     │
                    │  availability, resolve,     │
                    │  D10 isTransportRetryable   │
                    └─────────────┬───────────────┘
                                  │
         probes (async)           │  mode
    ┌─────────────────────────────┼────────────────────────────┐
    │                             ▼                            │
    │                   ┌──────────────────┐                   │
    │                   │  DialEpisode     │◄── Connect / claim│
    │                   │  coordinator     │◄── reconnect      │
    │                   │  (client)        │◄── Settings       │
    │                   └────────┬─────────┘                   │
    │                            │                             │
    │              ┌─────────────┴─────────────┐               │
    │              ▼                           ▼               │
    │     _openSocketDirect          _openSocketViaRelay       │
    │              │                           │               │
    └──────────────┴───────────────────────────┘               │
                     SettingsStore (sticky, selection, relay_*) │
                     ConnectionLifecycle (generation bump)     │
```

### 1.1 New pure module

**File:** `apps/mobile/lib/data/protocol/transport_policy.dart`

```dart
enum TransportMode { mesh, relay }

class TransportAvailability {
  const TransportAvailability({
    required this.meshConfigured,
    required this.relayConfigured,
    required this.meshOperational,
    required this.relayOperational,
  });
  final bool meshConfigured;
  final bool relayConfigured;
  final bool meshOperational;
  final bool relayOperational;

  bool get meshAvailable => meshConfigured && meshOperational;
  bool get relayAvailable => relayConfigured && relayOperational;
  bool get bothAvailable => meshAvailable && relayAvailable;
  bool get noneAvailable => !meshAvailable && !relayAvailable;
  TransportMode? get soleAvailable {
    if (meshAvailable && !relayAvailable) return TransportMode.mesh;
    if (relayAvailable && !meshAvailable) return TransportMode.relay;
    return null;
  }
}

/// Interactive path (menu or sole). Ignores sticky when sole available.
TransportMode? resolveInteractive({
  required TransportAvailability a,
  TransportMode? userSelection, // null = default Mesh when both
});

/// Whether DialEpisode may try the other transport after [code].
bool isTransportRetryable(String? code);

/// Permanent codes that must never trigger transport fallback (D10).
const transportPermanentCodes = { ... };

const transportRetryableCodes = { ... };
```

**Configured inputs** (no I/O):

```dart
TransportAvailability fromConfig({
  required String? host,
  required String? relayUrl,
  required String? relayHostId,
  required String? relayAuthority,
  required bool meshOperational,
  required bool relayOperational,
});
// relayConfigured only if url+hid non-empty AND authority matches host
```

### 1.2 Probe helpers

| Probe | API | Implementation notes |
|-------|-----|----------------------|
| Mesh | Keep `McremoteClient.probeDirectReachable` | Optionally thin-wrap in policy package for tests |
| Relay | `Future<bool> probeRelayReachable(String relayBase, {Duration timeout})` | `GET` https healthz derived from base; accept 200 + body containing `"ok"`; timeout ~900ms; parallel with mesh |

**Relay healthz URL:** add `RelayTransport.healthzUrl(String relayBase)` next to
`phoneWsUrl` (`relay_transport.dart:380`). `phoneWsUrl` normalizes to
`wss://…/v1/phone` and must **not** be reused here; healthz is
`https://<authority>/healthz` on the same base.

**Relay probe TLS policy (explicit):** set `badCertificateCallback => true`,
mirroring `probeDirectReachable` (`mcremote_client.dart:718`). Rationale: the
probe is a **soft liveness signal only** — no secret is sent, and the real
security boundary is the inner mcremote TLS pin, which is unchanged. A pinned
probe would fail closed on any relay cert rotation and hide an available
transport.

**Parallel wall budget:** `Future.wait([mesh, relay])` with each capped at 900ms → ~1s wall.

Place relay probe next to mesh probe (same file as client statics **or**
`transport_probes.dart`) so widget tests can inject fakes.

### 1.3 DialEpisode coordinator (client-owned)

**Inside `McremoteClient`** (keeps socket ownership; policy stays pure):

```dart
// Pseudo
Future<void> _runDialEpisode({
  required String hostInput,
  required Future<void> Function(TransportMode mode, _EpisodeCtx ctx)
      dialAndHandshake,
  required TransportMode primary,
  TransportMode? alternateIfConfigured,
  required int networkGeneration,
  required bool allowTransportFallback,
  required bool userInitiated,   // A5: exempt from generation budget
  required bool oneShotCredential, // A1: true for claimPairCode
  void Function(String)? onProgress,
});

/// Per-episode mutable state shared with the leg callback.
class _EpisodeCtx {
  int epoch = 0;         // bumped ONCE per episode, not per leg
  bool claimSent = false; // A1: set the moment the pair.claim frame is written
}
```

State fields to add:

| Field | Role |
|-------|------|
| `_networkGeneration` | int; bumped by lifecycle |
| `_fallbackSpentGeneration` | int?; generation for which fallback already ran |
| `_lastTransportSuccess` | memory cache of sticky (also prefs) |
| `_activeTransport` | mode of current live socket (for Settings) |
| `_episodeInFlight` | guards re-entrancy; asserts single episode |

**Rules:**

- Enter episode → cancel reconnect timer; bump `_connectEpoch` **once**.
- **Epoch ownership invariant (was unspecified):** `_connectEpoch` is bumped
  exactly **once per episode**, not once per leg. Today both
  `claimPairCode` (`mcremote_client.dart:904`) and `_connectInternal`
  (`:1067`) bump on entry; the alternate leg must re-enter them with the
  episode's existing epoch (pass `_EpisodeCtx.epoch` in, or split an
  `_attemptWithEpoch` helper out of each) so the first leg's teardown does not
  observe its own successor as a supersession. Every existing `_staleAttempt`
  check keeps working unchanged.
- If `_reconnectInFlight` and caller is another reconnect → supersede or no-op
  per existing patterns; **never** two concurrent episodes.
- Fallback only if all hold:
  - `allowTransportFallback`
  - `isTransportRetryable(code)` (§1.4)
  - alternate configured
  - **A1:** `!(oneShotCredential && ctx.claimSent)` — a pair code may already
    be consumed host-side, so never resend it on another transport (§0.3 #6)
  - **A5:** `userInitiated || _fallbackSpentGeneration != _networkGeneration`
- On fallback attempt set `_fallbackSpentGeneration = _networkGeneration`
  (user-initiated episodes also set it, but do not consult it).
- On auth/pair success: persist sticky; clear handshake failure count.

**A1 in practice — where `claimSent` flips:** in the claim path the frame is
written just after `afterSocketOpen` runs (`mcremote_client.dart:~930`). Set
the latch there. Consequence: for `claimPairCode` the automatic alternate
covers `connect_failed`, `cert_mismatch`, `relay_connect_failed`,
`relay_join_failed`, `relay_outer_error` — and **not** `auth_timeout`, which
by definition happens after the code left the device. On a post-claim failure,
surface the error with re-pair guidance ("the code may have been used — ask
the host for a new one") rather than offering Try-other, which would spend the
code again and land on permanent `invalid_code`.

**Episode progress + wall-clock cap (§0.3 #9):** per-leg progress is published
on `McremoteClient.dialProgress` ("Connecting over mesh…", "Mesh failed —
trying relay…") so a mesh→relay episode is legible instead of a silent ~28s
stall (mesh `ready` 8s + relay `ready` 20s + ~1s probes).

**As built, the 35s cap is a deadline on *starting* the alternate leg, not a
timer raced against the dial.** Racing a `.timeout()` against an in-flight
socket open is precisely how a socket gets orphaned (MADR 0046 H-A): the
channel's futures only resolve when its `HttpClient` is closed, so a timeout
that fires mid-open leaves nobody owning the cleanup. Each leg is already
bounded by its own `ready`/request timeouts, which puts the true worst case
inside the budget anyway; the deadline is applied where it is safe, and the
episode refuses to begin a second leg once it has passed.

**Wire every entry:**

| Entry | Primary source | `allowTransportFallback` | `userInitiated` | `oneShotCredential` |
|-------|----------------|--------------------------|-----------------|---------------------|
| `connect(..., transport:)` | Argument | true (default) | true | false |
| `claimPairCode(..., transport:)` | Argument | true, **pre-claim only (A1)** | true | **true** |
| `reconnect` / `reconnectFromStore` | sticky or Mesh default | true | false | false |
| Settings Reconnect now | Forced selection | true | **true** | false |
| Try-other button | Forced other | true | **true** | false (hidden entirely after `claimSent`) |
| `_scheduleReconnect` timer | sticky | true | false | false |

**A5 — why `userInitiated` exempts the generation budget:** the budget exists
to stop a *machine* thrashing mesh↔relay under a connectivity storm (D11). A
human tapping **Try Relay** or **Reconnect now** is self-rate-limiting and is
the user's explicit instruction. Under a strict per-generation budget the
automatic fallback would consume the budget and then silently deny the user's
own Try-other its one alternate — the failure mode G4 was written to prevent,
inverted into a dead button. Background entries still respect the budget.

### 1.4 Align D10 with existing permanent sets

Do **not** replace `_permanentAuthCodes` (0046 reconnect park).

Add:

```dart
bool isTransportRetryable(String? code) {
  if (code == null) return true; // unknown → one hop
  if (transportConfigErrorCodes.contains(code)) return true; // A2 — see below
  if (transportPermanentCodes.contains(code)) return false;
  if (_isPermanent(code, isPair: false) || _isPermanent(code, isPair: true)) {
    return false; // auth/pair permanent never hop
  }
  return true; // permanent denylist; unknown → retryable once
}
```

**Union of both permanent sets is deliberate.** `_isPermanent` is consulted
for `isPair: false` **and** `isPair: true`, so pair-permanent codes
(`expired`, `unavailable`, `invalid_code`) also block a hop on a *token*
connect even though they cannot normally arise there. This fails **closed** —
the cost of a missed hop is one surfaced error, the cost of a wrong hop is a
burnt credential (§0.3 #6). Intentional; do not "tighten" it later without
re-reading this paragraph.

**A2 — config errors hop, they do not strand.**

```dart
const transportConfigErrorCodes = {'relay_misconfigured', 'no_transport'};
```

These say *"this transport is unusable as configured"*, which is the strongest
possible argument for trying the **other** one — not for giving up. The
realistic trigger is mundane: sticky says relay, `setRelayRoute` cleared the
route on an authority change, `_openSocketViaRelay` throws
`relay_misconfigured` (`mcremote_client.dart:544`) and a reachable mesh is
never dialled. Rules:

- `relay_misconfigured` / `no_transport` → **hop** to the other configured
  transport (still one alternate only).
- Surface `no_transport` to the user **only** once both transports are
  exhausted or unconfigured.
- Leave the `permanent: true` flag on the thrown `McException` alone — it is
  0046's background-reconnect park and is a separate axis from transport
  fallback. A2 changes only `isTransportRetryable`.

**D10 permanent (transport hop forbidden):**  
`invalid_token`, `expired`, `invalid_code`, `cert_mismatch`,
`client_key_required`, `client_key_mismatch`, `bad_version`, `unauthorized`,
`unavailable`, `no_credentials`, `pair_failed`, `rate_limited`.

- `rate_limited` — **lock:** no hop. Hopping transport does not placate a host
  that is rate-limiting, and on a claim it spends the code. Keep it out of
  `_permanentAuthCodes`, which 0046 L-3 deliberately trimmed for background
  reconnect; the two sets are independent.
- `pair_failed` does **double duty** today: "pairing superseded"
  (`mcremote_client.dart:920/936/951/…`) and a generic `permanent: true`
  catch-all (`:1056`). Both are classified permanent here. Superseded must
  never hop (a newer episode owns the epoch); the catch-all is arguably
  retryable but is post-claim by construction, so A1 would forbid the hop
  anyway. Net: no behavioural loss, and no need to split the code.
- `cert_mismatch` (`:146`), `relay_misconfigured` (`:544`), `no_transport` and
  `rate_limited` are **not** in either existing permanent set, which is why
  `transportPermanentCodes` exists as its own list rather than reusing
  `_permanentAuthCodes`.

**Explicitly retryable examples:**  
`connect_failed`, `relay_join_failed`, `host_offline`, `relay_connect_failed`,
`relay_outer_error`, `relay_buffer_overflow`, plus `relay_misconfigured` /
`no_transport` via A2.

**`auth_timeout` is conditionally retryable (A1):** retryable for `connect`
(token auth is idempotent — resending the same token over the other transport
is safe), **not** for `claimPairCode`, where it can only occur after the code
left the device. Enforced by the `oneShotCredential && claimSent` gate in
§1.3, not by the code set — `isTransportRetryable('auth_timeout')` stays true.

### 1.5 SettingsStore additions

| Key | Type | API |
|-----|------|-----|
| `last_transport_success` | string `mesh`\|`relay` | `get/setLastTransportSuccess` — **or** map by authority if multi-host; v1 may store single + clear on host change |
| `transport_selection` | string optional | `get/setTransportSelection` |

**v1 authority model (actionable):** store sticky/selection as plain strings
tied to current host authority; in `setHost` / authority change / `clearAll` /
`setRelayRoute` clear when authority mismatches (mirror relay_authority).

Extend `clearAll` (`settings_store.dart:713`) to remove new keys.

**Test-fake churn is smaller than it looks.** `FakeSettingsStore`
(`test/connect_screen_test.dart:12`) and `_FakeStore`
(`test/settings_screen_test.dart:11`) **extend** `SettingsStore`, so they
inherit the new accessors for free — override only where a test needs to
control sticky. The fake that genuinely must change is `FakeMcremoteClient`
(`test/connect_screen_test.dart:66`), which overrides `connect` /
`claimPairCode` and so must track their signatures.

### 1.6 ConnectionLifecycle

On connectivity change that triggers reconnect, and on resume-driven reconnect:

```dart
client.bumpNetworkGeneration();
await client.reconnectFromStore(store);
```

Document: generation bump **before** reconnect so a flurry of events shares one
generation if coalesced; if events are far apart, each resume is a new
generation (new fallback budget) — acceptable.

Optional debounce: coalesce connectivity events 500ms (nice-to-have P3).

---

## 2. UI contracts

### 2.1 ConnectScreen state machine (D5)

```text
credentials known
       │
       ▼
  run probes (busy: "Checking transports…")
       │
       ├─ bothAvailable ──► show Transport control
       │                    default selection = sticky if available else Mesh
       │                    if arrived via QR/code-with-relay: STOP (wait Connect)
       │                    if user tapped Connect / sole was false: wait click
       │
       ├─ soleAvailable ──► chip Using Mesh|Relay
       │                    if QR/code auto path: DialEpisode auto-start
       │                    if manual: enable Connect only
       │
       └─ none ───────────► disable Connect; Retry probes
```

**Breaking change vs today:** dual-available QR does **not** call
`_claimCode`/`_connect` until Connect.

**Code-only enter without relay:** meshConfigured only → sole mesh path after
probe (or none).

**Paste/scan with relay:** both probes → menu; user confirms.

**Auto-connect on `_load` with saved token:** treat as reconnect entry
(sticky primary + DialEpisode), not as “menu session”. Do not show menu mid
auto-connect; if dual available, use sticky or Mesh default without waiting.

### 2.2 Transport control widget

Extract small private or shared widget used by Connect + Settings:

- `SegmentedButton<TransportMode>` or `DropdownButtonFormField`
- Labels: **Mesh**, **Relay**
- Subtitle/helper: mesh → host authority; relay → `hid=` + short relay host
- Visible iff `bothAvailable`

### 2.3 Settings

Replace/extend Route ListTile:

- Last success mode
- Probe chips (refresh button)
- Transport segmented control when both available
- **Reconnect now** button → `client.reconnect(..., transport: selected, forced: true)` API

### 2.4 Error actions

On connect/claim failure card:

- If other transport **configured** (not necessarily probe-pass): **Try Mesh**
  / **Try Relay**
- Forces new DialEpisode with that primary (`userInitiated: true`, so A5
  grants it an alternate even if the automatic one already ran this generation)
- **A1 exception — hide Try-other entirely when the failed episode had
  `claimSent`.** The code is likely spent host-side; another transport would
  resend it and land on permanent `invalid_code`. Show instead: *"The pair code
  may have been used. Ask the host for a new code."* with a Rescan/Enter-code
  action. This is the one place the UI must **not** offer the other transport.

---

## 3. API deltas (call sites)

### 3.1 `McremoteClient`

```dart
Future<void> connect({
  required String hostInput,
  required String token,
  String? fingerprint,
  TlsMode? mode,
  TransportMode? transport, // null → resolve sticky/default inside
  String? relayUrl,
  String? relayHostId,
  bool enableAutoReconnect = true,
  bool allowTransportFallback = true,
});

Future<String> claimPairCode({
  required String hostInput,
  required String code,
  ...
  TransportMode? transport,
  String? relayUrl,
  String? relayHostId,
  bool allowTransportFallback = true,
});

Future<void> reconnect({
  String? hostInput,
  String? token,
  TransportMode? transport, // forced when non-null
  bool allowTransportFallback = true,
});

void bumpNetworkGeneration();
TransportMode? get activeTransport;
TransportMode? get lastTransportSuccess;
```

Remove public reliance on “relay args imply force relay”.  
`_openSocket` takes explicit `TransportMode` (required for each leg).

**Amendment A3 — `transport` is nullable here, `required` in MADR D7.**
Deliberate: background entries (`reconnectFromStore`, `_scheduleReconnect`,
cold auto-connect) have no UI to resolve a mode and would otherwise each
duplicate the sticky→config ladder at the call site. `null` means "resolve
inside via §4 P0.5 background rules"; a non-null value is still authoritative,
so D7's guarantee — *no leg dials without an explicit mode* — holds at the
`_openSocket` boundary, which is where it matters. Record as an amendment to
D7 rather than shipping a silent divergence.

### 3.2 Call sites to update

| Site | Change |
|------|--------|
| `connect_screen.dart` | Probes, menu, state machine, pass mode |
| `app_lifecycle.dart` | `bumpNetworkGeneration` before reconnect |
| `settings_screen.dart` | Transport UI + reconnect |
| `FakeMcremoteClient` in tests | New params |
| Any other `connect(` / `claimPairCode(` | grep and fix |

---

## 4. Phased delivery

### Phase P0 — Pure policy + probes (no UI behaviour change yet)

**Deliverables:**

- `transport_policy.dart` + unit tests (`test/transport_policy_test.dart`)
- `probeRelayReachable` + `RelayTransport.healthzUrl` + unit/integration smoke
  (mock HttpServer optional)
- Document D10 sets next to existing permanent codes, including
  `transportConfigErrorCodes` (A2) and the union rationale

**Exit:**

- Matrix tests for `resolveInteractive` (all rows in MADR D3)
- `isTransportRetryable` covers permanent denylist
- `flutter test test/transport_policy_test.dart`

**Non-exit:** no ConnectScreen change yet (optional behind unused API).

---

### Phase P0.5 — DialEpisode in client (behaviour change: failover)

**Deliverables:**

- `_runDialEpisode` in `mcremote_client.dart`
- `connect` / `claimPairCode` / `_connectInternal` / `_scheduleReconnect` /
  `reconnect` use episode with mode resolution
- Default mode when null: sticky if set else mesh if both configured else sole
  configured path (config without probe for background — **background uses
  sticky/config without requiring dual probe pass**)
- Generation fields + `bumpNetworkGeneration`
- Persist sticky on success via SettingsStore
- `claimSent` latch (A1) + episode progress/cap (§1.3)
- Remove `_shouldUseRelay` force-on-attempt behaviour — **but only together
  with the probe-backed interactive resolution below**

**Background mode resolution (no UI probes):**

```text
if sticky configured for host → sticky
else if relay configured → prefer mesh if host set else relay
else mesh
```

Fallback still allows hop to other **configured** transport on retryable fail.

#### Do not ship G2 bare — the off-mesh QR regression

Commit `cd592a1` made an **attempt-scoped** relay tuple force the relay
(`_shouldUseRelay`, `mcremote_client.dart:644`) precisely so an off-mesh QR
pairing dials the relay immediately. G2 deletes that. But the background rules
above resolve a fresh QR claim — no sticky yet, host always populated — to
**mesh first**, which off-mesh means an 8s `ready` timeout before the relay is
tried. That is strictly worse than today for the exact scenario 0061 fixed,
and §7's claim that P0.5 "improves off-mesh even before menu" is false for it.

**Resolution — P0.5 must include probe-backed interactive resolution.** Pull
the P0 probe pair forward into the interactive entries (`connect`,
`claimPairCode`) now; only the *menu* waits for P2:

```text
interactive entry with both transports configured:
  run parallel probes (§1.2, ~1s wall)
  → resolveInteractive(availability, userSelection: null)   // Mesh when both
  → sole available wins outright; no 8s dead wait off-mesh
background entry: unchanged (sticky/config ladder, no probes)
```

The alternative — keeping attempt-force-relay until P2 — is acceptable if P2
follows immediately, but it leaves a known-wrong heuristic live and makes G2 a
P2 gate rather than a P0.5 one. **Prefer probes forward;** it is the same code
P2 needs and it makes P0.5 independently shippable, which was the whole point
of sequencing failover ahead of the menu.

**Exit:**

- Unit/fake tests: primary fail retryable → one alternate success; permanent →
  no alternate; second fail stops
- **A1:** claim fails post-`claimSent` (`auth_timeout`) → **zero** alternate;
  claim fails pre-claim (`connect_failed`) → one alternate
- **A2:** `relay_misconfigured` with mesh configured → hops to mesh
- **A5:** budget spent by an automatic fallback still permits a
  `userInitiated` episode its own alternate
- Epoch invariant: an episode's alternate leg does not mark the primary leg
  stale (assert `_connectEpoch` bumps once across a two-leg episode)
- Off-mesh QR claim with relay configured does **not** wait on a mesh dial
- Concurrent reconnect: only one episode (use existing client tests patterns)
- `flutter test test/mcremote_client_test.dart test/socket_dial_failure_test.dart`
  green (update expectations)

**Risk:** changing reconnect path may flake live-ish tests; prefer fakes.

---

### Phase P1 — Explicit TransportMode API stabilization

**Deliverables:**

- Public API as in §3.1 finalized
- Call sites compile; Fake clients updated
- `ConnectionPath` either deprecated for dial or updated to take
  `TransportMode` for status strings only

**Exit:** analyze clean; no force-relay from QR args alone.

---

### Phase P2 — ConnectScreen transport UX

**Deliverables:**

- Probe on host/relay change and after pair populate
- Transport control when both available
- Sole chip + auto-start only when sole available and path is QR/code auto
- Dual available: **no** auto-claim; require Connect
- Wire Connect/claim to selected mode + DialEpisode
- Status strings “Transport: Mesh|Relay”
- Try-other on failure

**Exit widget tests:**

| Test | Expect |
|------|--------|
| Paste dual-relay URI, both probes true (inject) | Menu visible; claim/connect **not** called until Connect |
| Sole relay available | Auto claim/connect with relay mode |
| Mesh only | No menu; mesh mode |
| Default selection | Mesh when dual and no sticky |
| Sticky relay + dual | Menu defaults to Relay highlight |

Inject probes via `@visibleForTesting` hooks or constructor overrides on a
small `TransportProbe` interface to avoid real network in widget tests.

**Exit:** `flutter test test/connect_screen_test.dart` (+ new cases).

---

### Phase P3 — Lifecycle generation + sticky reconnect polish

**Deliverables:**

- `app_lifecycle` bumps generation
- Cold auto-connect uses sticky DialEpisode
- Document/test multi-trigger: two connectivity events same gen do not grant
  two fallbacks after one already spent

**Exit:** `lifecycle_policy_test` + client generation unit test.

---

### Phase P4 — Settings parity

**Deliverables:**

- Route section: last success, probes, control, Reconnect now
- Reconnect now = forced primary DialEpisode

**Exit:** `settings_screen_test` updates; manual ops checklist.

---

## 5. File-level checklist

| Path | P0 | P0.5 | P1 | P2 | P3 | P4 |
|------|----|------|----|----|----|-----|
| `lib/data/protocol/transport_policy.dart` | **C** | | | | | |
| `lib/data/protocol/connection_path.dart` | | | U | | | |
| `lib/data/ws/mcremote_client.dart` | U probes | **U** | U | | U | |
| `lib/data/ws/relay_transport.dart` | U `healthzUrl` | | | | | |
| `lib/data/ws/mc_exception.dart` | U taxonomy helper | | | | | |
| `test/mc_exception_test.dart` | U D10 sets | | | | | |
| `lib/data/local/settings_store.dart` | | U sticky | | | | U |
| `lib/features/connect/connect_screen.dart` | | | | **U** | | |
| `lib/features/settings/settings_screen.dart` | | | | | | **U** |
| `lib/app_lifecycle.dart` | | | | | **U** | |
| `test/transport_policy_test.dart` | **C** | | | | | |
| `test/dial_episode_test.dart` | | **C** | | | | |
| `test/connect_screen_test.dart` | | | | U | | |
| `test/mcremote_client_test.dart` | | U | U | | U | |
| `test/settings_screen_test.dart` | | | | | | U |
| `test/relay_path_test.dart` | | | U | | | |
| `docs/0061-MADR-…` | | | | | | note supersession |
| `docs/0062-MADR-…` | | | | | | status → implemented when done |

C = create, U = update.

---

## 6. Testing strategy

### 6.1 Unit (no sockets)

- Full D3 interactive matrix
- DialEpisode state machine (table-driven)
- Authority mismatch → relay not configured
- Generation fallback budget

### 6.2 Client fakes

- Fake channel open failures by mode (mesh fails, relay succeeds)
- Assert single alternate call
- Permanent `invalid_token` → zero alternate
- **A1:** `claimPairCode` where the mesh leg opens, sets `claimSent`, then
  fails `auth_timeout` → assert the relay leg is **never dialled** and the code
  is never re-sent (this is the credential-burn regression test)
- **A2:** primary relay → `relay_misconfigured`, mesh configured → mesh dialled
- **A5:** automatic fallback spends the generation budget; a subsequent
  `userInitiated` episode in the same generation still gets its alternate
- Mesh-only pair (no relay tuple) → `RelayTransport.open` never invoked
  (promote manual row 10 into a unit assertion)
- Epoch invariant: two-leg episode bumps `_connectEpoch` exactly once

### 6.3 Widget

- Probe injection interface
- QR dual / sole / none flows
- Settings reconnect button invokes forced mode (mock client)

### 6.4 Manual ops checklist (hybrid device)

Run these from
[ops-hardware-validation.md](ops-hardware-validation.md), which carries the same
rows with macOS **and** Linux commands and the service-manager pitfalls that
silently invalidate them.


| # | Scenario | Pass |
|---|----------|------|
| 1 | On-mesh, dual probe pass → menu → Mesh connect | |
| 2 | Same → user Relay connect | |
| 3 | Off-mesh, mesh probe fail, relay pass → auto Relay, no menu | |
| 4 | Dual pass false mesh later: Mesh dial fails → automatic one Relay success | |
| 5 | QR dual: no auto-claim until Connect | |
| 6 | Kill mesh mid-session → reconnect sticky → fallback once | |
| 7 | Spam airplane mode → no mesh↔relay thrash loop | |
| 8 | invalid_token → no transport hop; re-pair guidance | |
| 9 | Settings Reconnect now with Relay forced while sticky Mesh | |
| 10 | Mesh-only pair (no relay in URI) → never opens RelayTransport | |
| 11 | Claim over mesh, kill link **after** code is sent → no relay retry; "code may have been used" copy; fresh code pairs cleanly (A1) | |
| 12 | Sticky relay + relay route cleared → hops to mesh, not stranded (A2) | |
| 13 | Off-mesh QR-with-relay claim connects without an ~8s mesh stall (P0.5) | |
| 14 | Mesh→relay episode shows "Mesh failed — trying relay…", never a silent ~28s stall | |

---

## 7. Implementation order (actionable day plan)

1. **Land P0** policy + tests (reviewable PR or commit alone).
2. **Land P0.5** DialEpisode behind existing connect without UI menu — behaviour:
   sticky/default mesh + failover improves off-mesh even before menu.
3. **Land P1** API cleanup.
4. **Land P2** Connect UX (largest product-visible change).
5. **Land P3** lifecycle generation.
6. **Land P4** Settings.
7. Update 0061 supersession note + 0062 status when complete.
8. Rebuild mobile app for device validation (ops checklist).

Suggested commit boundaries match phases; each phase must leave `flutter test`
green for touched tests + `dart format` on edited Dart.

---

## 8. Acceptance gates (definition of done)

| Gate | Criterion |
|------|-----------|
| G1 | MADR D1–D5, D10–D11 behaviour covered by automated tests |
| G2 | No `_shouldUseRelay` attempt-force-relay path remains |
| G3 | Dual-available QR requires Connect; sole auto-starts |
| G4 | DialEpisode: ≤1 automatic alternate **per episode**, and ≤1 across all **background** episodes in a network generation. User-initiated episodes (Try-other, Settings Reconnect now) are exempt from the generation budget and keep their own single alternate (A5) |
| G5 | Permanent codes never alternate; `relay_misconfigured` / `no_transport` **do** alternate when the other transport is configured (A2) |
| G6 | Settings can force transport + reconnect |
| G7 | Manual checklist §6.4 rows 1–14 signed off on hybrid hardware |
| G8 | `dart format` + `flutter analyze` clean on `apps/mobile` |
| G9 | A pair code is never sent over a second transport within one episode (A1) — covered by an automated test, not just the manual row |
| G10 | Off-mesh QR-with-relay pairing is no slower after G2 than before it |
| G11 | Every multi-leg episode reports per-leg progress and fails by 35s |

---

## 9. Open implementation choices (narrow; not product re-litigation)

Resolve during P0/P0.5 coding, default recommended:

| Choice | Recommendation |
|--------|----------------|
| Sticky storage multi-host map vs single | **Single** + clear on host authority change (v1) — amendment **A4** to D8's "per authority"; behaviourally identical for one host, revisit if multi-host lands |
| `isTransportRetryable` denylist vs allowlist | **Denylist permanent** (unknown → retryable once) |
| Try-other new network generation? | **Resolved (A5):** same generation, but `userInitiated: true` exempts it from the budget so it always gets its one alternate. Supersedes the earlier "same gen" note, which silently produced a dead button after an automatic fallback |
| Background mode without probes | sticky → else mesh if host else relay if configured |
| Interactive mode in P0.5, before the menu | **Probes forward** (§4 P0.5) — required to avoid the off-mesh QR regression |
| Probe injection | `typedef TransportProbes` or class with static defaults |
| Split `pair_failed` into superseded vs generic? | **No** — both stay permanent; A1 already forbids the only hop the generic case could want (§1.4) |

---

## 10. Relationship to MADR 0062

This plan keeps every locked **product** decision (D1–D3, D5, D6, D9, D11
intent). It:

- Grounds them in file/function names
- Sequences DialEpisode **before** menu UI (P0.5 before P2) so failover ships
  even if UI slips
- Makes QR deferral and concurrency concrete
- Aligns D10 with existing `McException.permanent` without breaking 0046

If review rejects a sequencing detail (e.g. wants menu before DialEpisode),
product rules still hold; only phase order changes.

### 10.1 Amendments to fold back into 0062-MADR

An earlier draft claimed this plan changed *no* locked decision. That was not
accurate — five decisions need amendment. Approving this plan means
approving these; update the MADR before P4 closes.

| ID | Touches | Amendment | Why |
|----|---------|-----------|-----|
| **A1** | D4, D10 | Automatic fallback for `claimPairCode` is allowed on **pre-claim** failures only (`claimSent` latch) | Pair codes are one-shot (`internal/auth/paircode.go:190`); a post-claim hop burns the code and strands the user with a token minted host-side but never delivered |
| **A2** | D10 | `relay_misconfigured` / `no_transport` move from permanent to **hop-allowed** | A config fault on one transport is the best reason to try the other; as written, a cleared relay route strands a reachable mesh |
| **A3** | D7 | `transport` is nullable (`null` = resolve inside) rather than `required` | Background entries have no UI to resolve a mode; the explicit-mode guarantee still holds at `_openSocket` |
| **A4** | D8 | Sticky/selection stored as a single value cleared on authority change, not a per-authority map | Equivalent for v1's single host; smaller surface |
| **A5** | D11 | User-initiated episodes are exempt from the per-generation fallback budget | Under a strict budget the automatic fallback consumes it and disables the user's own Try-other — the D5 flow contradicts itself without this |

A1 and A2 are behavioural and safety-relevant; A3–A5 are mechanical. None
change what the operator sees in the menu.

---

## 11. Review checklist for approvers

- [ ] Dual-pass menu + DialEpisode + QR defer match operator intent
- [ ] Background sticky without dual probe is acceptable
- [ ] D10 permanent list complete for known `McException.code` values
- [ ] Phase order P0 → P0.5 → P2 acceptable
- [ ] Manual checklist §6.4 sufficient for ship
- [ ] No server work accidentally implied
- [ ] **Amendments A1–A5 (§10.1) accepted, and 0062-MADR will be updated**
- [ ] **A1:** never resending a pair code on a second transport is the right
      trade — the alternative recovers some flaky-mesh claims but can burn the
      operator's one-shot code
- [ ] **A2:** hopping on `relay_misconfigured` does not mask a real
      misconfiguration the operator should see (it is still surfaced when both
      transports fail)
- [ ] **P0.5:** probes pulled forward is preferred over keeping
      attempt-force-relay until P2
- [ ] 35s episode cap is right for the 8s-mesh + 20s-relay worst case
