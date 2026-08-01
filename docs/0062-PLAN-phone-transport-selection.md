# MADR 0062 — Implementation plan: phone transport selection

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to
[0062-MADR-phone-transport-selection.md](0062-MADR-phone-transport-selection.md).
This is the **review and build plan**: it re-grounds the MADR against the
current Flutter tree, names concrete APIs/files/tests, sequences work so policy
ships before UI, and defines acceptance gates.

- **Status:** Proposed for review (not implemented)
- **Date:** 2026-08-01
- **Scope:** Flutter mobile app only (`apps/mobile`). No mcremote/mcrelay
  protocol or pair URI wire changes.
- **MADR lock-ins (must not regress):** D1–D11 including Socratic amendments
  (DialEpisode, D10 taxonomy, QR defer when dual-available, network-generation
  fallback budget, Settings forced primary).
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
6. **Claim before token:** first path uses `claimPairCode`; DialEpisode must
   wrap socket open **before** `pair.claim`, then auth path for token connect.
7. **Cold auto-connect** (`ConnectScreen._load`) must use DialEpisode with
   sticky primary, not raw `connect` without mode.

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
| Relay | `Future<bool> probeRelayReachable(String relayBase, {Duration timeout})` | `GET` https healthz derived from base (mirror `SettingsStore.healthzUrl` pattern on relay URL); accept 200 + body containing `"ok"`; timeout ~900ms; parallel with mesh |

**Parallel wall budget:** `Future.wait([mesh, relay])` with each capped at 900ms → ~1s wall.

Place relay probe next to mesh probe (same file as client statics **or**
`transport_probes.dart`) so widget tests can inject fakes.

### 1.3 DialEpisode coordinator (client-owned)

**Inside `McremoteClient`** (keeps socket ownership; policy stays pure):

```dart
// Pseudo
Future<void> _runDialEpisode({
  required String hostInput,
  required Future<void> Function(TransportMode mode) dialAndHandshake,
  required TransportMode primary,
  TransportMode? alternateIfConfigured,
  required int networkGeneration,
  required bool allowTransportFallback,
});
```

State fields to add:

| Field | Role |
|-------|------|
| `_networkGeneration` | int; bumped by lifecycle |
| `_fallbackSpentGeneration` | int?; generation for which fallback already ran |
| `_lastTransportSuccess` | memory cache of sticky (also prefs) |
| `_activeTransport` | mode of current live socket (for Settings) |

**Rules:**

- Enter episode → cancel reconnect timer; bump `_connectEpoch` as today.
- If `_reconnectInFlight` and caller is another reconnect → supersede or no-op
  per existing patterns; **never** two concurrent episodes.
- Fallback only if `allowTransportFallback && isTransportRetryable(code) &&
  alternate configured && _fallbackSpentGeneration != _networkGeneration`.
- On fallback attempt set `_fallbackSpentGeneration = _networkGeneration`.
- On auth/pair success: persist sticky; clear handshake failure count.

**Wire every entry:**

| Entry | Primary source | `allowTransportFallback` |
|-------|----------------|--------------------------|
| `connect(..., transport:)` | Argument | true (default) |
| `claimPairCode(..., transport:)` | Argument | true |
| `reconnect` / `reconnectFromStore` | sticky or Mesh default | true |
| Settings Reconnect now | Forced selection | true |
| Try-other button | Forced other | true (new episode; new generation optional — prefer **same** gen if still in error UI so user gets one more try without resetting budget mid-storm; **document:** Try-other forces primary and allows one fallback within episode only) |
| `_scheduleReconnect` timer | sticky | true; respects generation |

### 1.4 Align D10 with existing permanent sets

Do **not** replace `_permanentAuthCodes` (0046 reconnect park).

Add:

```dart
bool isTransportRetryable(String? code) {
  if (code == null) return true; // unknown → one hop
  if (transportPermanentCodes.contains(code)) return false;
  if (_isPermanent(code, isPair: false) || _isPermanent(code, isPair: true)) {
    return false; // auth/pair permanent never hop
  }
  return true; // or whitelist retryableCodes only — prefer: permanent denylist
}
```

**D10 permanent (transport hop forbidden):**  
`invalid_token`, `expired`, `invalid_code`, `cert_mismatch`,
`client_key_required`, `client_key_mismatch`, `relay_misconfigured`,
`no_transport`, `bad_version`, `unauthorized`, `unavailable`,
`no_credentials`, `pair_failed` (superseded), `rate_limited` for **pair code**
claims only (optional: allow hop on rate_limited network — **lock:** no hop on
`rate_limited`).

**Explicitly retryable examples:**  
`connect_failed`, `auth_timeout`, `relay_join_failed`, `host_offline`,
`relay_connect_failed`, `relay_outer_error`, `relay_buffer_overflow`.

### 1.5 SettingsStore additions

| Key | Type | API |
|-----|------|-----|
| `last_transport_success` | string `mesh`\|`relay` | `get/setLastTransportSuccess` — **or** map by authority if multi-host; v1 may store single + clear on host change |
| `transport_selection` | string optional | `get/setTransportSelection` |

**v1 authority model (actionable):** store sticky/selection as plain strings
tied to current host authority; in `setHost` / authority change / `clearAll` /
`setRelayRoute` clear when authority mismatches (mirror relay_authority).

Extend `clearAll` to remove new keys.

FakeSettingsStore in tests must implement new methods.

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
- Forces new DialEpisode with that primary

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
- `probeRelayReachable` + unit/integration smoke (mock HttpServer optional)
- Document D10 sets next to existing permanent codes

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
- Remove `_shouldUseRelay` force-on-attempt behaviour

**Background mode resolution (no UI probes):**

```text
if sticky configured for host → sticky
else if relay configured → prefer mesh if host set else relay
else mesh
```

Fallback still allows hop to other **configured** transport on retryable fail.

**Exit:**

- Unit/fake tests: primary fail retryable → one alternate success; permanent →
  no alternate; second fail stops
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
| `lib/data/ws/relay_transport.dart` | U health helper | | | | | |
| `lib/data/ws/mc_exception.dart` | U? taxonomy helper | | | | | |
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

### 6.3 Widget

- Probe injection interface
- QR dual / sole / none flows
- Settings reconnect button invokes forced mode (mock client)

### 6.4 Manual ops checklist (hybrid device)

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
| G4 | DialEpisode: ≤1 automatic alternate per network generation per in-flight episode |
| G5 | Permanent codes never alternate |
| G6 | Settings can force transport + reconnect |
| G7 | Manual checklist §6.4 rows 1–10 signed off on hybrid hardware |
| G8 | `dart format` + `flutter analyze` clean on `apps/mobile` |

---

## 9. Open implementation choices (narrow; not product re-litigation)

Resolve during P0/P0.5 coding, default recommended:

| Choice | Recommendation |
|--------|----------------|
| Sticky storage multi-host map vs single | **Single** + clear on host authority change (v1) |
| `isTransportRetryable` denylist vs allowlist | **Denylist permanent** (unknown → retryable once) |
| Try-other new network generation? | **No** — same gen; forced primary new episode still one fallback max inside episode |
| Background mode without probes | sticky → else mesh if host else relay if configured |
| Probe injection | `typedef TransportProbes` or class with static defaults |

---

## 10. Relationship to MADR 0062

This plan **does not change** locked product decisions. It:

- Grounds them in file/function names
- Sequences DialEpisode **before** menu UI (P0.5 before P2) so failover ships
  even if UI slips
- Makes QR deferral and concurrency concrete
- Aligns D10 with existing `McException.permanent` without breaking 0046

If review rejects a sequencing detail (e.g. wants menu before DialEpisode),
product rules still hold; only phase order changes.

---

## 11. Review checklist for approvers

- [ ] Dual-pass menu + DialEpisode + QR defer match operator intent
- [ ] Background sticky without dual probe is acceptable
- [ ] D10 permanent list complete for known `McException.code` values
- [ ] Phase order P0 → P0.5 → P2 acceptable
- [ ] Manual checklist §6.4 sufficient for ship
- [ ] No server work accidentally implied
