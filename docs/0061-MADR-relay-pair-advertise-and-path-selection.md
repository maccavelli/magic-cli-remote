# MADR 0061: Relay pair advertise vs register, and phone path selection

- **Status**: Accepted — implemented 2026-08-01
- **Date**: 2026-08-01
- **Deciders**: Project Owner
- **Scope**: mcremote `relay` config validation and serve vs pair semantics;
  mobile connect-screen population of relay routes from pair QR/URI; attempt-
  scoped relay path selection in `McremoteClient`. Does **not** change mcrelay
  join-plane protocol, trust model, or mesh-as-fallback on reconnect.
- **Related**:
  [0015-MADR-mcrelay-transport-security.md](0015-MADR-mcrelay-transport-security.md)
  (opaque splice, pair URI `relay`/`hid`, mesh when available),
  [0016-MADR-mcrelay-audit-hardening.md](0016-MADR-mcrelay-audit-hardening.md)
  (reachability probe R7),
  [ops-mcrelay.md](ops-mcrelay.md) (wire mcremote to mcrelay),
  [config.md](config.md) (`relay.*` keys and env),
  [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md)
  (LaunchAgent env for secrets).
- **Extends**: 0015 D (pair discovery) and phone join path — does not reopen
  the outer/inner TLS or auth model.

## Decision summary

Split **advertise** credentials (`relay.url` + `relay.host_id`) from
**register** credentials (those plus `relay.secret`):

1. Config load accepts url+host_id without a secret so `mcremote pair` can
   emit `relay=` / `hid=` from YAML while the registration secret lives only
   in the serve process environment (LaunchAgent / systemd).
2. `mcremote serve` still requires a complete registration tuple
   (`CanRegister()`) when `relay.url` is set; incomplete credentials fail
   closed at daemon start.
3. On the phone, an **attempt-scoped** relay route from the current pair
   QR/paste always selects the relay path (no mesh-preference probe for that
   attempt). Stored routes on reconnect still prefer mesh when the direct
   probe succeeds (0015 fallback).
4. The connect UI shows Host as the **mcremote** authority and a separate
   Relay line when the QR carried `relay`+`hid`, so operators can see that
   the outer join path was populated.

## Context and problem statement

mcrelay was deployed at a public edge (`wss://…:8443`). mcremote registered
successfully as host `macos-laptop` via LaunchAgent env
(`MCREMOTE_RELAY_*`). Pairing and off-mesh connect still failed or looked
like “mesh only”:

| Symptom | Observed cause |
|---------|----------------|
| Phone kept dialling the tailnet IP | Pair URI or saved credentials had no usable relay route, **or** the client preferred direct when a mesh probe succeeded |
| Host field looked “wrong” after scan | By design Host is mcremote (inner TLS); relay was not shown anywhere on the connect screen |
| `mcremote pair` without shell secret failed or omitted relay | Config validation required url, host_id, **and** secret together; secret lived only in the LaunchAgent, not the operator shell |
| Enter-code / re-pair quirks | Attempt flags and `setRelayRoute(null, …)` could clear in-memory relay after a code-only claim |

0015 already defined the pair URI fields and the mesh-when-available
fallback. It did **not** define how config is split across pair CLI vs serve
process, or how the phone treats a QR that explicitly carries a relay route
during first connect.

## Decision drivers

- Pair QR must include `relay` + `hid` whenever the host is configured for
  mcrelay, without forcing the registration secret into the shell or into
  `config.yaml`.
- Serve must not pretend to register when the secret is missing.
- A phone scanning a relay-bearing QR is expressing off-mesh intent for
  **that** attempt; a flaky or partially up Tailscale path must not steal
  the connect onto an unreachable mesh IP.
- Host (inner identity / pin) and Relay (outer join) must stay visually and
  storage-wise distinct — never write the public relay URL into Host.
- Reconnect after a successful pair may still prefer mesh when the phone is
  on the mesh (0015).

## Decision outcome

### D1 — Advertise vs register for `relay` config

| Concern | Required fields | Who needs them |
|---------|-----------------|----------------|
| **Advertise** (pair URI / QR) | `url`, `host_id` | `mcremote pair` (any process that loads config) |
| **Register** (outbound `/v1/host`) | `url`, `host_id`, `secret` (min 16) | `mcremote serve` only |

- `RelayConfig.Enabled()` remains “URL non-empty” (pair should emit relay
  routing when URL+host_id are present; validation keeps them paired).
- `RelayConfig.CanRegister()` is true only when URL, host_id, and secret are
  all non-empty.
- `validate()`:
  - url and host_id both set or both empty;
  - secret, if set, requires url+host_id and length ≥ 16;
  - secret alone is rejected.
- Daemon: if `Enabled()` and not `CanRegister()`, **serve returns an error**
  (fail closed). Do not start a half-configured relayhost client.

Preferred layout:

```yaml
# ~/.config/mcremote/config.yaml (mode 0600)
relay:
  url: "wss://relay.example.com:8443"
  host_id: "macos-laptop"
  secret: ""   # not required here
```

```bash
# LaunchAgent / systemd Environment=
MCREMOTE_RELAY_SECRET=…   # min 16; allowlist match on mcrelay
# optional overrides also fine:
# MCREMOTE_RELAY_URL=…
# MCREMOTE_RELAY_HOST_ID=…
```

### D2 — Attempt-scoped relay always wins on first connect

In `McremoteClient._shouldUseRelay`:

| Input | Behaviour |
|-------|-----------|
| Non-empty attempt `relayUrl` **and** `relayHostId` (this QR/paste) | **Use relay** — do not probe mesh |
| Partial attempt (only one of url/hid) | Do **not** invent a path; do not fall through to a stale stored route |
| No attempt args; stored route for same mcremote authority | Prefer **direct** if `probeDirectReachable`; else relay (0015 / 0016 R7) |

Rationale: connect-screen status already said “via relay …” when
`ConnectionPath.resolve(…, directReachable: false)` saw a relay QR, but the
socket opener still preferred mesh when the probe succeeded. That mismatch
is a bug relative to operator expectation and off-mesh pairing.

Reconnect (no attempt args) keeps mesh preference when the direct path is
healthy.

### D3 — Connect UI population and hint lifecycle

1. **Host field** = mcremote authority only (scheme + mesh/LAN host:port).
   Label clarifies “Host (mcremote)”.
2. When the QR has a full relay tuple, show a read-only **Relay (mcrelay)**
   line: `url · hid=…`.
3. Set `_pendingFor` and relay attempt fields **before** assigning
   `_hostCtrl.text`, so `_onHostEdited` does not treat the QR fill as a user
   edit that clears pin/relay hints.
4. `_attemptRelaySpecified` is true only when `payload.hasRelay` (not on
   every pair apply).
5. `claimPairCode` calls `setRelayRoute` only when the attempt passed
   non-null relay args — never wipe a good in-memory route with nulls after
   a code-only claim against the same host.

Storage after success is unchanged: `SettingsStore.setRelayRoute` under the
mcremote authority for later reconnects.

### D4 — Operator pairing contract (phone)

| Action | Relay route? |
|--------|----------------|
| Scan QR / paste full `mcremote://pair?…&relay=…&hid=…` | Yes (attempt-scoped) |
| Enter 8-char code only | **No** — needs Host + prior relay hints; code alone has no join metadata |
| Auto-reconnect with stored token | Stored route; mesh preferred if direct reachable |

Documented guidance: for off-mesh, clear old credentials, mint a pair that
prints a `Relay:` line, then **Scan QR** or paste the full URI.

## Consequences

### Positive

- `mcremote pair` works from YAML advertise fields without exporting the
  registration secret in the operator shell.
- Serve still hard-fails if registration secret is missing while url is set.
- Relay-bearing QR connects via mcrelay even when Tailscale is partially up.
- Operators can see that relay was populated without mistaking Host for the
  public edge.

### Negative / trade-offs

- A phone that is **fully** on mesh and scans a relay QR uses the slightly
  longer relay path for that attempt (acceptable; reconnect can go direct).
- Code-only entry remains mesh/host-field dependent; fixing that would need
  a different discovery story (out of scope).
- Config and docs must keep the advertise/register split clear so operators
  do not expect serve to register with an empty secret.

### Neutral

- mcrelay allowlist, outer TLS, and inner pin/client-key rules unchanged
  (0015).
- Pair URI encoding (`pairuri`, Flutter `PairPayload`) unchanged; only when
  and how consumers honour the fields changed.

## Implementation (this change set)

| Area | Change |
|------|--------|
| `internal/config/config.go` | `CanRegister()`; relaxed `validate()`; comments on advertise vs register |
| `internal/daemon/daemon.go` | Serve fails if `Enabled()` && !`CanRegister()` |
| `apps/mobile/.../mcremote_client.dart` | Attempt-scoped relay always; claim no longer null-wipes route |
| `apps/mobile/.../connect_screen.dart` | Relay display; hint order; `hasRelay`-only attempt flag |
| `docs/config.md` | Secret required for serve registration only |
| Tests | `TestRelayValidateAdvertiseWithoutSecret`; connect_screen paste-relay case |

## Verification

- `go test ./internal/config/ ./internal/daemon/`
- `flutter test test/connect_screen_test.dart test/relay_path_test.dart`
- Live: mcrelay `/healthz` ok; mcremote log `registered with mcrelay`;
  `mcremote pair code` prints `Relay: … (host_id=…)` and URI contains
  `relay=` + `hid=` without shell secret when YAML has url+host_id and
  LaunchAgent has `MCREMOTE_RELAY_SECRET`.

## Follow-ups (non-blocking)

- Rebuild/install mobile app so phones pick up D2/D3 (host binary alone is
  not enough).
- Optional: advanced connect UI to paste relay URL + hid for code-only
  flows without a full QR.
- Optional: soft-warn in pair output when config can advertise but the
  running daemon was started without `CanRegister()` (ops drift).

## Rejected alternatives

| Alternative | Why rejected |
|-------------|--------------|
| Require secret in YAML for pair | Defeats env-only secret hygiene; encourages committing secrets |
| Always force relay on reconnect | Breaks 0015 “mesh when available” for day-to-day on-mesh use |
| Put relay URL in Host field | Conflates outer and inner identity; breaks pin authority and pair.claim target |
| Soften serve to start without secret | Silent non-registration; phones join `host_offline` with no host-side error |
