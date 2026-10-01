---
status: proposed
date: 2026-08-19
decision-makers: Project Owner
consulted: none
informed: operators editing mcremote config.yaml
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Add a `display_name` config parameter so the phone shows a friendly host name instead of an IP address

## Context and Problem Statement

The phone app's sessions landing screen identifies the connected host with
whatever the phone dialled. On a mesh install that is a Tailscale IPv4
address, so the header reads e.g. "Connected to 100.64.0.9 over mesh". The
request is to let the operator set

```yaml
display_name: <friendly host name>
```

in mcremote `config.yaml` so the phone shows that name instead.

This record assesses the technical feasibility of that change and proposes
the design for review. No implementation is included.

### What the phone shows today, and where it comes from

The sessions-screen label is derived client-side from the dialled endpoint —
the daemon is not consulted for it:

* `_hostnameOf` extracts the host part of the saved endpoint:
  `SettingsStore.parseEndpoint(input).host`, falling back to the literal
  word "host" (`apps/mobile/lib/features/sessions/sessions_screen.dart:1289-1296`).
* `_connLabel` interpolates that string into the connected and stale
  strings users see — "Connected to $hostname over mesh", "Checking
  connection to $hostname…" (`sessions_screen.dart:1298-1331`). Connecting,
  reconnecting, error, and disconnected states do **not** interpolate the
  hostname (they are "Connecting…", "Reconnecting to host…", etc.).
* The label is rendered at the top of the sessions screen both in the
  offline/linking `ConnBanner` and in the connected-state chip
  (`sessions_screen.dart:1423-1432`, `1491-1496`).
* The dialled host arrives through `client.hostInputListenable`, a
  `ValueNotifier` on the WS client
  (`apps/mobile/lib/data/ws/mcremote_client.dart:508-512`).

Why an IP: `pair.advertise_host` defaults to auto-detect — Tailscale IPv4,
else loopback (`configs/config.example.yaml:277-283`). The phone dials that
address and then displays its host part. A `letsencrypt` install shows the
cert domain instead, which is the same mechanism producing a nicer value,
not a separate code path.

The settings "Connection & security" Host row shows the **raw stored
endpoint** (`_host`, e.g. `10.0.0.5:7531`), not `_hostnameOf`
(`apps/mobile/lib/features/settings/settings_screen.dart:1263-1269`).
`_loadConnectionInfo` (`:203-268`) only loads that stored value; it is not
the render site.

Foreground-service and chat banners still say the generic word "host"
(`foreground_service.dart:86`, `notification_coordinator.dart:288`,
`chat_screen.dart:2270`). Those surfaces never showed the dialled address
and are out of scope for this change.

* Implementation Plan: [0102-PLAN-host-display-name-config.md](./0102-PLAN-host-display-name-config.md)

### The existing channel for daemon-reported host metadata

The daemon already sends one display-adjacent host fact to the phone: the
user's home directory, in the auth response.

* `protocol.AuthOKPayload` carries `home_dir` alongside `device_id` /
  `device_name` (`internal/protocol/messages.go:215-221`).
* The auth handler fills it (`internal/ws/server.go:994-999`). There is
  exactly one `AuthOKPayload{…}` construction site; v2 fields (`protocol`,
  `caps`, resume) are written onto that same struct afterwards.
* The phone reads it into `hostHomeDir` right after auth and clears it when
  the dialled host's authority changes
  (`mcremote_client.dart:501-503`, `2214-2215`, `2276-2283`).

This is the exact shape a `display_name` needs: an informational,
operator-owned string, reported by the daemon when the socket becomes
authenticated, consumed by the phone as UI input, optional on both ends.

First enrollment does **not** go through `auth_ok`. `pair.claim` succeeds
with `pair_ok` (`PairOKPayload` at `messages.go:257-264`, filled at
`server.go:1160-1164`) and the phone marks itself connected on that frame
(`mcremote_client.dart:1956-2001`) without a subsequent auth. `home_dir` is
absent from `pair_ok` today (new-session UI is later); `display_name` is
shown on the sessions header immediately after pairing, so `pair_ok` must
carry it too or the first landing still shows the IP.

### Constraints any new config key ships with (MADR 0090)

The config surface is guarded by reflection-based parity tests: every
`mapstructure` key on `config.Config` must be spelled in **all four** YAML
templates — the setup-service seed (`internal/cli/service/defaults_mcremote.yaml`),
`configs/config.example.yaml`, `configs/config.prod.example.yaml`, and
`configs/config.mesh-grok.yaml` — and the seed and example must have
identical key sets (`internal/cli/service/template_parity_test.go:186-248`).
`docs/config.md` is the operator reference and says to keep `configs/*.yaml`
in sync. Viper's `AutomaticEnv` only resolves keys it already knows
(`internal/config/load.go:256-364`; `config_test.go:47-50` documents the
same gap for `receipts.*`). A new key therefore needs `SetDefault` even
when the default is empty. An explicit `BindEnv` is not what makes
`AutomaticEnv` work, but it matches the existing `data_dir` /
`MCREMOTE_DATA_DIR` pairing (`load.go:46`, `262`) and documents the alias.
These are CI-enforced mechanics, not design risks — but they define the
full file list up front.

## Feasibility findings

1. **The feature is feasible with a small, fully-precedented footprint.**
   Every layer already has the exact pattern in place: config field
   (`PairConfig.AdvertiseHost` shape), viper wiring (`setDefaults` +
   `BindEnv` as for `data_dir`), protocol field (`AuthOKPayload.HomeDir`
   shape, plus the same string on `PairOKPayload` for first enrollment),
   server plumbing (`ws.Options` → `Server` field, wired in
   `internal/daemon/daemon.go:314-332`), phone consumption
   (`hostHomeDir` shape plus a `ValueNotifier` so settings can rebuild
   without a connection-state change), and UI substitution (the one
   `_connLabel` call site that interpolates the hostname).
2. **Wire compatibility is free.** `auth_ok` and `pair_ok` are JSON; an
   added `display_name` field with `omitempty` is ignored by old phones
   (Dart reads map keys defensively, exactly as it does for `home_dir`),
   and a new phone against an old daemon sees the field absent and falls
   back to today's hostname derivation. `home_dir` is set identically for
   v1 and v2 clients, so no protocol-version bump is involved.
   `TestV1AuthOKIsByteIdentical` (`internal/ws/negotiation_test.go:56-101`)
   only forbids `protocol`/`caps` on v1 `auth_ok`; empty `display_name` is
   omitted by `omitempty` and leaves that test's wire bytes unchanged.
3. **The relay path needs no special handling.** Phones dialled through
   mcrelay still authenticate against the daemon's WS endpoint over the
   tunnel, so `auth_ok` / `pair_ok` — and therefore `display_name` —
   arrives the same way. The label code never inspects the transport for
   the name; transport stays where it is ("… over relay" suffix).
4. **No interaction with pairing or TLS.** `pair.advertise_host` and
   `tls.letsencrypt.domains` control *how the phone reaches the host*
   (routing, hostname verification). `display_name` is a pure label and
   must not be used for either — keeping them separate means letsencrypt
   hosts keep advertising their cert domain while still showing the
   friendly name.
5. **Config changes take effect on daemon restart.** There is no general
   hot-reload; `config.Live` exists only to persist phone-driven
   `prewarm` writes (`internal/config/prewarm_write.go:79-91`). Reading
   `display_name` once at server construction matches every other
   presentation setting the WS server carries (`Version`, `ListenAddr`,
   `HeadscaleURL`).
6. **Security surface is negligible.** The value travels inside the
   already-authenticated, TLS-protected WS session, is shown only to
   devices holding a valid device token, and is rendered by Flutter with
   `maxLines: 1` + ellipsis on the sessions chip. The only hygiene needed
   is trimming at load time and a length cap in `Validate` so a malformed
   config cannot ship an unbounded string into every auth/pair response.
   `Validate` is a value receiver (`config.go:1023`) and cannot persist a
   trim; `Load` must trim before `Validate` runs.
7. **Risks are cosmetic, not functional.** Worst case is a label the
   operator dislikes; the fallback (endpoint hostname) is today's
   behaviour in every combination of old/new daemon and phone.

## Decision Drivers

* Zero behaviour change when the key is unset — empty must reproduce
  today's label exactly, on every phone vintage.
* Reuse the existing daemon→phone metadata channel rather than inventing a
  new message or endpoint.
* Survive the MADR 0090 template-parity gate without exemptions.
* Work identically on direct-mesh and relay transports.
* Keep the label out of routing, TLS, and pairing semantics.
* First pairing must show the name: the sessions header is the first
  thing the phone renders after `pair_ok`, and that path never sees
  `auth_ok`.

## Considered Options

* Option 1: top-level `display_name` in config.yaml, reported on the
  frames that mark the socket authenticated (`auth_ok` and `pair_ok`)
* Option 2: nest it as `pair.display_name`
* Option 3: phone-side rename only — the user edits the label on each
  phone, no daemon change

## Decision Outcome

Chosen option: "Option 1: top-level `display_name` in config.yaml,
reported on the frames that mark the socket authenticated (`auth_ok` and
`pair_ok`)", because it is a host-identity fact reported whenever the
phone becomes authenticated (not a pairing-time artefact of
`advertise_host`), it reuses the `home_dir` precedent for reconnects and
extends the same string onto `pair_ok` so the first sessions landing is
not a special case, and it is the only option that satisfies the actual
request — one setting in config.yaml that every paired phone picks up.

### Shape of the chosen option

* `internal/config/config.go`: top-level `DisplayName string
  \`mapstructure:"display_name"\`` on `Config`, immediately after
  `DataDir` (currently line 22); empty default; `Load` trims whitespace
  after unmarshal; `Validate` errors past 128 Unicode characters (runes,
  not bytes). `Validate` is a value receiver and does not trim in place.
* `internal/config/load.go`: `SetDefault("display_name", d.DisplayName)`
  (required so `AutomaticEnv` sees the key) and
  `BindEnv("display_name", "MCREMOTE_DISPLAY_NAME")` (same pairing as
  `data_dir` / `MCREMOTE_DATA_DIR`).
* `internal/protocol/messages.go`: `DisplayName string
  \`json:"display_name,omitempty"\`` on both `AuthOKPayload` (after
  `HomeDir`, currently line 221) and `PairOKPayload` (after
  `DeviceName`, currently line 261). Filled for v1 and v2 alike.
* `internal/ws/server.go` + `internal/daemon/daemon.go`: `DisplayName`
  option on the WS server, stamped into both success payloads
  (`server.go:994-999` and `:1160-1164`).
* Phone: `hostDisplayName` on the WS client mirroring `hostHomeDir` —
  read from `auth_ok` (`mcremote_client.dart:2214-2215`) **and**
  `pair_ok` (`:1956-1957`), cleared in `_noteHost` (`:2276-2283`),
  surfaced for rebuilds via a `ValueNotifier` the same way
  `hostInputListenable` is. `sessions_screen.dart` passes
  `display name if non-empty, else _hostnameOf(hostInput)` into
  `_connLabel`. The settings Host row keeps the endpoint as a second
  line for diagnostics and puts the friendly name above it.
* Templates and docs (CI-enforced): `display_name: ""` with a comment in
  `internal/cli/service/defaults_mcremote.yaml`,
  `configs/config.example.yaml`, `configs/config.prod.example.yaml`,
  `configs/config.mesh-grok.yaml`; reference entry in `docs/config.md`;
  a row in the README YAML-surface table; a contract note in
  `docs/protocol-v1.md` on both `auth_ok` and `pair_ok`.

### Consequences

* Good, because the operator sets one value and every paired phone —
  current and future — shows it after its next auth **or** after first
  pairing, with no per-phone setup.
* Good, because old phones and old daemons are unaffected: unknown JSON
  fields are ignored, absent fields fall back to today's hostname.
* Good, because the relay path inherits the behaviour with no relay-side
  change.
* Bad, because a renamed host only reaches a phone on its next
  authenticated frame — a live session keeps the old label until
  reconnect, re-pair, or app restart. This is inherent to reporting the
  name at auth/pair time and matches `home_dir`.
* Bad, because the four YAML templates and docs must move in lockstep —
  accepted, and enforced by the existing parity test rather than by care.

### Confirmation

* Go unit tests: `display_name` loads from YAML and
  `MCREMOTE_DISPLAY_NAME`, is trimmed by `Load` and capped by `Validate`,
  and appears in `auth_ok` (v1 and v2) and `pair_ok` — extending the
  existing `config` and `ws` test files.
* The MADR 0090 parity tests must pass without adding `display_name` to
  `omittedConfigKeys`.
* Flutter widget/state tests: label prefers the display name when present,
  falls back to the endpoint hostname when absent, is captured from both
  `auth_ok` and `pair_ok`, and clears when the host authority changes.
* Manual acceptance: set `display_name`, restart the daemon, reconnect the
  phone — the sessions-screen header reads "Connected to <name> over
  <transport>"; unset it and the IP label returns. A first pair against a
  named daemon must also show the name without a second connect.

## Pros and Cons of the Options

### Option 1: top-level `display_name`, reported on `auth_ok` and `pair_ok`

* Good, because it matches the request literally: one key in config.yaml.
* Good, because `auth_ok` already carries a display-adjacent host string
  (`home_dir`), so the daemon, wire, and phone changes each copy a working
  pattern instead of introducing a new one.
* Good, because `pair_ok` is the only success frame the phone sees on
  first enrollment, and the sessions header is on screen immediately
  afterwards.
* Good, because the value is host identity, not pairing presentation — it
  belongs next to `data_dir` / `log`, not under a phase-specific section.
* Neutral, because it takes effect at next authenticated frame; labels do
  not need mid-session refresh.
* Bad, because it widens the config surface by one key, which the MADR
  0090 machinery then obliges across four templates and docs — the cost is
  mechanical, not conceptual.

### Option 2: nest as `pair.display_name`

* Good, because `PairConfig` already governs "how the daemon presents
  itself to phones", so the key sits near related levers.
* Bad, because pairing is a one-time event and `pair.advertise_host`
  changes what the phone *dials*; a name shown on every reconnect is a
  runtime identity fact, and nesting it invites exactly the routing/TLS
  conflation Decision Driver 5 rules out.
* Bad, because it would still need the same protocol and phone changes —
  the nesting buys nothing but a longer key path.

### Option 3: phone-side rename only (no daemon change)

* Good, because it touches no Go code, protocol, or templates.
* Bad, because it fails the request as stated: there is nothing to put in
  config.yaml.
* Bad, because every phone must be renamed separately, a factory-reset or
  new device loses the label, and different phones on the same host can
  disagree about its name.

## More Information

* Touch-point inventory for the eventual plan (all confirmed against
  current code, 2026-08-19):
  * `internal/config/config.go` (struct + `Validate`),
    `internal/config/load.go` (`setDefaults`, `BindEnv`, trim after
    unmarshal), `internal/config/config_test.go` (existing `Load` tests
    live here; there is no `load_test.go`)
  * `internal/protocol/messages.go` (`AuthOKPayload`, `PairOKPayload`)
  * `internal/ws/server.go` (`Options`, `Server`, auth handler, pair
    handler), `internal/daemon/daemon.go` (wiring),
    `internal/ws/negotiation_test.go` (`newNegotiationServer` +
    `dialNegotiation`; `writeEnv`/`readEnv` are in `server_test.go`,
    same `ws_test` package), `internal/ws/server_test.go`
    (`TestWSPairClaim` at line 472 is the pair-code harness to copy)
  * `internal/cli/service/defaults_mcremote.yaml` (`data_dir` at line 32),
    `configs/config.example.yaml` (line 58),
    `configs/config.prod.example.yaml` (line 60),
    `configs/config.mesh-grok.yaml` (line 62), `docs/config.md` (settings
    table `data_dir` at line 84; env table `MCREMOTE_DATA_DIR` at line
    353), `docs/protocol-v1.md` (`auth_ok` example at 180-182, `pair_ok`
    example at 208-219), `README.md` YAML-surface table at 717-733
  * `apps/mobile/lib/data/ws/mcremote_client.dart`,
    `apps/mobile/lib/features/sessions/sessions_screen.dart`,
    `apps/mobile/lib/features/settings/settings_screen.dart`, plus Dart
    tests (`mcremote_client_test.dart` `_AuthServer.start`,
    `sessions_screen_test.dart` MockMcremoteClient,
    `settings_screen_test.dart` `_FakeClient` / `pumpSettings`)
* Out of scope (confirmed, not missing):
  * Chat linking banner ("Connecting to host…"), FGS /
    notification-coordinator "Connected to host" copy — they never showed
    the dialled address.
  * Any `mcremote` CLI flag (`data_dir` has `--data-dir`; this key does
    not get a parallel flag).
  * `GET /v1/hello` (`server.go:474-500`) — unauthenticated, not a
    display channel.
* Review questions resolved (2026-08-18, Project Owner):
  1. Length cap — 128 characters; `Validate` errors beyond it (rune
     count).
  2. Settings-screen connection info adopts the display name in this same
     change (as a first line above the endpoint, not a replacement).
  3. Env alias `MCREMOTE_DISPLAY_NAME` is in scope.
* Assessment findings (2026-08-19, grounded in current tree — see
  [0102-PLAN](./0102-PLAN-host-display-name-config.md)):
  1. `pair_ok` must carry `display_name`; first enrollment never sees
     `auth_ok`.
  2. `Validate` cannot trim (`func (c Config) Validate() error` is a
     value receiver); `Load` trims.
  3. `SetDefault` is what makes `AutomaticEnv` resolve the env var;
     `BindEnv` is the `data_dir` pairing, not a second registration
     requirement.
  4. Load tests belong in `config_test.go` (existing `TestPairAdvertiseHost`
     / `TestLoadFileAndEnv` pattern). There is no `TestHelloReportsTLS`
     and no `authConn` in `negotiation_test.go`.
* Pre-approval status: per the MADR/plan workflow, this record proposes;
  the implementation plan (`0102-PLAN-host-display-name-config.md`) is
  written and approved before any code changes begin.
