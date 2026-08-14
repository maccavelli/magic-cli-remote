---
status: proposed
date: 2026-08-13
decision-makers: Project Owner (scope and acceptance); Implementer (measurement)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Select grok ACP auth methods from the live initialize catalog and write keys where grok actually reads them

## Context and Problem Statement

Every Grok prewarm and session spawn logs:

```text
agent advertises auth methods but none configured; session/new may fail
  component=provider.grok count=2
```

MADR 0072 F9 treated that line as noise and offered two remediations: set
`providers.grok.auth_method_id`, or silence the warning because
"sessions still created successfully today". Both are incomplete. The
comment next to the emit
(`internal/provider/acpagent/acpagent.go:456-472`) still says the skip
is "correct for grok today", and
`internal/config/config.go:476-479` still says an empty
`auth_method_id` is "correct for agents that need none (grok)".

Those claims were true of grok **0.2.112** as assessed in
[0038](./0038-MADR-grok-acp-parity-assessment.md) (`cached_token` +
`grok.com`, session/new worked without `authenticate`). They are not
true of **grok 1.0.3 (`1a29d5bc12d4`)** as measured on this host
2026-08-13.

Two independent auth surfaces have grown apart:

| Layer | IDs today | Who consumes them |
| --- | --- | --- |
| ACP `initialize.authMethods` + `authenticate {methodId}` | `xai.api_key`, `cached_token`, `grok.com` (dynamic; see §probe) | `acpagent.spawnAgent` only if `Config.AuthMethodID` is set |
| Phone `provider.Auth` catalog (MADR 0074 / 0083) | `xai:api`, `xai:device` | `grok/auth.go` + `grok/device_auth.go` |

They do not share identifiers, they do not share a store, and the
phone write path lands a key in a TOML table grok 1.0.3 does not read.

This record asks: **which ACP methods must the daemon invoke, how must
they be chosen, and what store/status work has to land before the
phone's grok credential flows are actually complete?**

Probe target: `grok 1.0.3 (1a29d5bc12d4) [stable]` at
`~/.grok/bin/grok` (same binary the LaunchAgent resolves). Daemon at
the probe: `mcremote 0.10.8.3.g99770be`,
`providers.grok.auth_method_id: ""`, `prewarm: true`. ACP SDK:
`github.com/coder/acp-go-sdk v0.13.5` (`ProtocolVersionNumber = 1`).

## Decision Drivers

* The live grok binary is the contract (AGENTS.md; MADR 0050 D2 / 0081).
  Official ACP schema and `~/.grok/docs/user-guide/02-authentication.md`
  are supporting evidence only.
* Headless-first (MADR 0074): no browser on the host, no SSH, no secret
  in argv, no mcremote-owned vault (0074 D2).
* Never auto-invoke an advertised method that hangs or opens a browser
  on a cold host. A static `auth_method_id` is in that class.
* Every method the phone offers must either complete or be marked
  unavailable up front (MADR 0083 D4). A write that grok ignores is a
  silent lie.
* Do not destabilise the logged-in SuperGrok path that already creates
  sessions today.
* Pin every asserted CLI/ACP shape with a `live_grok` test (0074 D15).

## Considered Options

* **O1 — Silence the warning, or pin `auth_method_id: cached_token`**
  (0072 F9 as written)
* **O2 — Handshake only:** pick a *safe* advertised ACP method at
  initialize time and call `authenticate`; leave the 0074 phone store
  as it is
* **O3 — Handshake plus store correction:** O2, and move the grok key
  write/status onto the tables and env grok 1.0.3 actually honours;
  map the phone catalog onto the ACP method ids without renaming the
  wire
* **O4 — Collapse the two catalogs** into one set of ACP method ids on
  the phone (`xai.api_key` / `cached_token` / `grok.com`) and drive
  everything through `authenticate`

## Decision Outcome

Chosen option: **"O3 — Handshake plus store correction"**, because O1
is unsafe on a cold host (session/new hangs or returns
`Authentication required`), O2 leaves the phone writing a key grok
never reads, and O4 would churn a shipped `provider_auth` capability
for no runtime gain — the phone already has working device-flow
chrome, it just needs a store grok accepts.

This MADR is **proposed** for review. It does not change code. The
companion plan is
[0085-PLAN-grok-acp-auth-method-wiring.md](./0085-PLAN-grok-acp-auth-method-wiring.md).

Review of the first draft (same day) locked three refinements that the
draft left open: D2 gained an explicit-override rule and an env/file
fallback; D4 writes the **current default model only**; D7 fails
`Start` before `session/new` instead of hoping grok returns `-32000`.

### Sub-decisions (proposed)

**D1 — `authMethods` is a live, credential-gated catalog. Never pin
one id in config as the grok default.**

On grok 1.0.3 the initialize list changes with what the process can
already use:

| Process state (isolated `HOME`/`GROK_HOME` unless noted) | Advertised `authMethods` | `_meta.defaultAuthMethodId` |
| --- | --- | --- |
| Cold, no credentials | `[grok.com]` | omitted |
| Cold + `XAI_API_KEY` in env | `[grok.com]` at initialize; `authenticate xai.api_key` still succeeds | omitted |
| Cold + quoted `[model."grok-4.5"] api_key` | `[xai.api_key, grok.com]` | `xai.api_key` |
| Host `~/.grok/auth.json` present, no `XAI_API_KEY` (LaunchAgent) | `[cached_token, grok.com]` | `cached_token` |
| Host `auth.json` + `XAI_API_KEY` in the caller env | `[xai.api_key, cached_token, grok.com]` | `cached_token` |

That is why the daemon logs `count=2` and an interactive shell with
`XAI_API_KEY` set sees three. 0038's two-method excerpt is stale.
`defaultAuthMethodId` is on initialize `_meta`;
`grokInitializeMeta` (`acpagent.go:893-900`) currently discards
everything except `modelState`.

**D2 — Call `authenticate` only with a method that is headless-safe.
Never send `grok.com` from the daemon.**

Selection, in order. "Advertised" means present in this process's
`initialize.authMethods`.

1. If `Config.AuthMethodID` is non-empty: use it **only** when it is
   advertised **and** is `cached_token` or `xai.api_key`. Any other
   value (including `grok.com`, a typo, or an unadvertised id) fails
   `spawnAgent` immediately with a typed error. This keeps the existing
   config knob as an operator pin without restoring 0072 F9's hang.
2. Else if `cached_token` is advertised, use it. On this host it
   returns in <2s with identity `_meta` (email, `auth_mode=Oidc`,
   `subscription_tier`, team ids). It is a confirmation, not a login.
3. Else if `xai.api_key` is advertised, use it. Success is `{}`.
4. Else if the process has a usable API key — `XAI_API_KEY` non-empty
   **or** a quoted `[model."<id>"] api_key` is present in
   `config.toml` — call `authenticate` with `xai.api_key` even when
   that id was not advertised. Isolated-home probe: cold + env key
   advertised only `grok.com`, but `authenticate xai.api_key`
   succeeded and `session/new` returned a `sessionId`.
5. Else do **not** call `authenticate`. Apply D7.

`AuthenticateRequest` is `{methodId}` only (ACP v1 schema;
`acp-go-sdk` `types_gen.go:705-715`). No secret travels on this RPC.
Secrets stay in grok's own store / env (0074 D2).

Do **not** implement this as "always send
`_meta.defaultAuthMethodId`": on a cold host that value is `grok.com`
or omitted, and `grok.com` is the hang.

**D3 — `grok.com` is host-only until a Strategy B successor.**

On a logged-in host, `authenticate grok.com` is a no-op that returns
the same identity `_meta` as `cached_token`. On a cold host it is the
interactive SpaceXAI OIDC flow (`grok login --oauth` / browser at
`auth.x.ai`). Mark it `available: false`, `reason: browser_only` on
the phone catalog (0083 D4). The phone's existing device path remains
`grok login --device-auth` (0074 W2, already wired).

ACP `logout` is **not** implemented (`-32601 Method not found`).
`agentCapabilities.auth` is the empty default `{}`. Phone
`clearCredential` must stay a file/env operation; there is no RPC to
pair it with.

**D4 — Fix the grok key write. `[auth] api_key` is not a grok 1.0.3
store.**

`SetGrokModelAPIKey` (`credstore/write.go:302-343`) comments that it
writes a "per-model `api_key`" and then writes the `[auth]` table.
`grok inspect --json` on an isolated home that contained
`[auth] api_key = "…"` still advertised only `grok.com`, and
`authenticate xai.api_key` returned:

```json
{"code":-32000,"message":"Authentication required",
 "data":"Set XAI_API_KEY or add api_key/env_key to config.toml."}
```

The same error for unquoted `[model.grok-4.5]`. `grok inspect`
reported `configWarnings: unknown-field` with `target=model`,
`key=grok-4`, `field=5` — TOML treated the dots as table path
separators.

The write that grok accepted, advertised as `xai.api_key`, set
`defaultAuthMethodId=xai.api_key`, and then allowed `session/new`:

```toml
[model."grok-4.5"]
api_key = "…"
```

`XAI_API_KEY` in the process environment also makes
`authenticate xai.api_key` succeed, but injecting it requires a
LaunchAgent restart the daemon cannot perform on itself (the reason
0074 D1 preferred a file write). Keep env as an operator escape, not
the phone path.

D4 therefore: write the pasted key under **exactly one** quoted
table, `[model."<current-default-id>"]`. Do not write `[auth]`. Do
not fan the key out across the catalog. Refuse `methodID` values
other than `xai:api` (already done, 0083 D2). Leave
`~/.grok/auth.json` untouched on key write and on key clear
(`grok/auth.go:44-46` is still correct).

**Current default** is resolved at write time, in this order:

1. `providers.grok.model` / `Config.Model` when non-empty — the
   operator pin. `ListModels` already treats this as
   `DefaultIDs[0]` (`acpagent.go` `withCatalogDefault`).
2. Else `Provider.ListModels(ctx).DefaultIDs[0]` — live
   `initialize._meta.modelState.currentModelId` once any grok
   process has initialized (prewarm or harvest). Probed: SuperGrok
   host → `grok-4.6`; isolated cold home → `grok-4.5`.
3. Else `ListModels` `Options[0].ID` (static floor first entry,
   `grok-4.6` in `grok.go` `staticModels`).
4. Else fail the write. Do not invent an id and do not fall back to
   `[auth]`.

A write also deletes a leftover `[auth] api_key` (the current
function's output) so a migrated host cannot look configured in the
file while grok ignores the table. It does not touch other
`[model."…"]` tables the user may have written by hand.

Clear removes `api_key` from the **same resolved default table** and
from leftover `[auth]`. It does not wipe every model table.
`AuthStatus` (D5) still treats *any* quoted `[model."…"] api_key` as
presence, so a stale table from a previous default still reads
configured.

**D5 — `AuthStatus` must observe the same stores grok uses.**

`authStatus` (`grok/auth.go:66-79`) is `configured` iff `XAI_API_KEY`
is non-empty **or** `~/.grok/auth.json` exists. It does not read
`config.toml`. After a correct D4 write on a host with no OAuth
session the phone would still show *missing*. After today's incorrect
`[auth]` write it also shows *missing*, which accidentally matches
reality.

Status becomes configured when any of: `auth.json` present,
`XAI_API_KEY` set, or a quoted `[model."<id>"] api_key` exists.
Never read key material into the status payload (0074 D2/D11).

**D6 — Keep the phone method ids. Map them onto ACP; do not rename
the wire.**

| Phone method (0074/0083) | ACP method | Drive path |
| --- | --- | --- |
| `xai:api` | `xai.api_key` | D4 file write; next spawn advertises it and D2 authenticates |
| `xai:device` | (none directly; produces `cached_token`) | existing `grok login --device-auth` (`device_auth.go:20-38`); next spawn advertises `cached_token` |
| — | `cached_token` | daemon-only handshake (D2); not a phone button |
| `xai:browser` | `grok.com` | catalog row, `type=oauth_browser` → `available:false` / `browser_only` via the existing 0083 D4 annotator (`ws/server.go` `upstreamAuthPayload`) |

`acpagent.StartDeviceAuth` already ignores `methodID`/`inputs`
(`acpagent.go:221`). That is acceptable while grok has one device
CLI. If a second device-shaped ACP method appears, thread `methodID`.

**D7 — If D2 selects nothing, fail `Start` before `session/new`. Do
not keep a prewarm spare that cannot create a session.**

Probed:

* Cold, no credentials, no `authenticate`: `session/new` did not
  return inside 15s (`startTimeout` is 30s). Hoping grok emits
  `-32000` is not a contract — it sometimes hangs instead.
* Cold, after a failed `authenticate xai.api_key`: `session/new`
  returned `-32000 Authentication required` /
  `"no auth method id provided"`.

So: after initialize, if D2 produced no method, `spawnAgent` records
"no headless-safe auth" on the session, `EnsureWarm` must **not**
retain that process as a spare, and `Start` returns a typed
auth-required error **without** calling `session/new`. The 0072
"sessions still work" observation is the logged-in host only.

### Consequences

* Good, because the prewarm warning becomes either `acp authenticated
  method_id=cached_token|xai.api_key` or a real error, never a
  standing false alarm.
* Good, because a phone-pasted xAI key starts working on a cold host
  without SSH and without a daemon restart.
* Good, because `grok.com` cannot be selected by a config typo into a
  headless hang.
* Neutral, because phone ids stay `xai:api` / `xai:device`; only the
  daemon maps them. Old phones keep working.
* Neutral, because `authenticate` `_meta` (email, tier) is available
  and unused; surfacing it on the provider chip is optional follow-up,
  not required to close this record.
* Neutral, because a key written under today's default does not follow
  a later model switch; the user pastes again, or uses `XAI_API_KEY`.
  That is the cost of not duplicating the secret.
* Bad, because D4 couples mcremote to grok's TOML quoting rules for
  dotted model ids. A live test must fail when grok stops honouring
  `[model."<id>"]`.

### Confirmation

1. Isolated `HOME`/`GROK_HOME`, no credentials: initialize advertises
   only `grok.com`; daemon does **not** call `authenticate` and does
   **not** call `session/new`; `Start` returns an auth-required error
   inside `startTimeout` (no hang). No prewarm spare is retained.
2. Isolated home + quoted `[model."grok-4.5"] api_key` when that is
   the live `currentModelId`: initialize advertises `xai.api_key`;
   daemon calls `authenticate` with that id; `session/new` returns a
   `sessionId`.
3. Host with `~/.grok/auth.json` and no `XAI_API_KEY` (the LaunchAgent
   case): initialize advertises `cached_token` + `grok.com`; daemon
   calls `authenticate cached_token`; log line is `acp authenticated`,
   not the current warning; existing SuperGrok sessions still start.
4. `SetCredential(…, "xai", "xai:api", key, nil)` writes **one**
   quoted `[model."<resolved-default>"]` table, deletes leftover
   `[auth] api_key` if present, and never writes a second model
   table. `ClearCredential` removes that table's `api_key` and any
   leftover `[auth] api_key`. `auth.json` is byte-identical.
5. After (4) on a host with no `auth.json` and no `XAI_API_KEY`,
   `AuthStatus` is `configured`.
6. Phone catalog: `xai:api` and `xai:device` available;
   `grok.com` visible and disabled with `browser_only`.
7. `go test -tags live_grok ./internal/provider/grok/ -run Auth` fails
   if initialize method ids, defaultAuthMethodId gating, quoted-table
   parse, or the `-32000` cold-start shape drift.
8. Pin 0038 / 0072 F9 / `config.go:476-479` comments as superseded by
   this record so the next reader does not re-apply O1.

## Pros and Cons of the Options

### O1 — Silence the warning, or pin `cached_token`

* Good, because it is a one-line config or log change.
* Bad, because `cached_token` is **not advertised** on a cold host;
  sending it hangs `authenticate`.
* Bad, because pinning `grok.com` (the only cold-host method) hangs
  or opens a browser on the host.
* Bad, because it leaves `SetGrokModelAPIKey` writing a table grok
  ignores, so 0083's "grok config-file key writes ✅" stays a lie.

### O2 — Handshake only

* Good, because it stops the warning on the common logged-in host and
  avoids the cold-host hang if `grok.com` is never auto-sent.
* Bad, because a phone-pasted key still lands in `[auth]` and never
  becomes `xai.api_key`.
* Bad, because `AuthStatus` still cannot see a successful file write.

### O3 — Handshake plus store correction

* Good, because it is the minimum that makes both ACP and the phone
  catalog true.
* Good, because it reuses the existing `xai:api` / `xai:device`
  chrome and the existing `StartCLIDeviceFlow` parser.
* Neutral, because D4 is a small, testable store change, not a new
  protocol. Writing one table keeps the secret in one place.
* Bad, because dotted model ids must be written quoted; that is easy
  to get wrong (the current function already got the table wrong).
* Bad, because the key does not follow a later default-model change.

### O4 — Collapse catalogs onto ACP ids

* Good, because one vocabulary.
* Bad, because it breaks shipped `provider_auth` payloads and phone
  fixtures for a rename that does not change behaviour.
* Bad, because `cached_token` is not a user action (no secret, no
  URL); putting it on the phone as a third method is noise.

## More Information

### Live ACP shapes (2026-08-13, grok 1.0.3)

Initialize methods, verbatim from `grok --no-auto-update agent --no-leader stdio`
with `protocolVersion: 1` (same argv and version the daemon uses):

```json
[
  {"id":"xai.api_key","name":"xai.api_key",
   "description":"XAI_API_KEY or api_key/env_key in config.toml"},
  {"id":"cached_token","name":"cached_token",
   "description":"Cached token from ~/.grok/auth.json"},
  {"id":"grok.com","name":"Grok","description":"Sign in with Grok"}
]
```

No `type` discriminator. `acp-go-sdk v0.13.5` therefore decodes every
row as `AuthMethod.Agent` (default when `type` is absent;
`types_gen.go:477-561`). A Go unmarshal of the captured initialize
result yields `len=3`, all `Agent`. The SDK is not dropping a method;
the daemon's `count=2` is the LaunchAgent process, which has
`auth.json` and no `XAI_API_KEY`.

`authenticate` results on this logged-in host:

| `methodId` | Result | Notes |
| --- | --- | --- |
| `cached_token` | `200` + `_meta` `{email, auth_mode:"Oidc", subscription_tier:"SuperGrok", team_id, …}` | <2s |
| `xai.api_key` | `200` `{}` when env/config key present; `-32000` with the "Set XAI_API_KEY…" string when not | |
| `grok.com` | `200` + same identity `_meta` as `cached_token` when already signed in; no return in 8s when cold | |
| `does-not-exist` | `-32602 Invalid params` / `unsupported auth method: does-not-exist` | |
| ACP `logout` | `-32601 Method not found` | |

`session/new` after a successful `cached_token` or `xai.api_key`
authenticate returns a `sessionId` and the usual `_x.ai/*`
notifications. It also succeeds *without* `authenticate` when
`auth.json` is present — that is today's daemon path.

Host `~/.grok/auth.json` (0600) is an OIDC session keyed by
`https://auth.x.ai::<client_id>`, `auth_mode=oidc`, with
`refresh_token` / `expires_at`. That is `cached_token`. This host's
`~/.grok/config.toml` has no `api_key` line.

### Provider code assessment

What is already correct:

* `grok` is a thin `acpagent.Spec` (`grok.go:49-95`) and reuses the
  shared initialize / session machinery.
* Phone device flow is the right CLI (`login --device-auth`),
  non-destructive per 0074, parsed by `providerauth.StartCLIDeviceFlow`.
* `SetCredential` already refuses `xai:device` and foreign method ids
  (`grok/auth.go:34-36`, `auth_test.go`).
* `ClearCredential` does not delete `auth.json`.
* `SupportsDeviceAuth` is true because `StartDeviceAuth` is set
  (`acpagent.go:215-217`).
* ACP `authenticate` plumbing exists and is the right RPC
  (`acpagent.go:461-468`). The gap is *which* id, and when.

What is wrong or incomplete:

| # | Gap | Evidence |
| --- | --- | --- |
| G1 | `AuthMethodID` is empty; every spawn warns | `config.yaml:69`; `acpagent.go:470-472`; today's `mcremote.err.log` |
| G2 | Comment claims grok needs no ACP auth | `acpagent.go:456-459`; `config.go:476-479` |
| G3 | Method choice is a static config string, not the live list | `Config.AuthMethodID`; 0072 F9 |
| G4 | `defaultAuthMethodId` is discarded | `grokInitializeMeta` only binds `modelState` |
| G5 | Phone ids ≠ ACP ids; no mapping | `xai:api` vs `xai.api_key` |
| G6 | Key write hits `[auth]`, which grok 1.0.3 ignores | `credstore/write.go:321-341`; isolated-home probe |
| G7 | Unquoted `[model.grok-4.5]` is parsed as `model.grok-4.5` | `grok inspect` `unknown-field` warning |
| G8 | `AuthStatus` ignores `config.toml` | `grok/auth.go:70-75` |
| G9 | 0083 A8 / activation matrix mark grok key writes as working | 0083 §More Information; contradicted by G6 |
| G10 | Cold `session/new` can hang for the full start timeout | isolated-home probe |
| G11 | `StartDeviceAuth` drops `methodID` | `acpagent.go:221` — acceptable until a second device method exists |

0074 D1's *intent* ("Grok → `XAI_API_KEY` or per-model `api_key` in
`config.toml`") is right. The implementation drifted to `[auth]`.
0074 D19 (grok is single-upstream `xai`) stays. 0074 Strategy E
(`auth_provider_command`) stays declined.

### Official ACP notes (supporting, not decisive)

* v1 `authenticate` carries only `methodId`. Called when the agent
  requires auth before session creation
  ([schema](https://agentclientprotocol.com/protocol/v1/schema)).
* `session/new` *may* return `auth_required`. grok 1.0.3 sometimes
  does (`-32000`) and sometimes stalls instead.
* A `terminal` method must not be sent to `authenticate`
  ([RFD](https://agentclientprotocol.com/rfds/auth-methods)). grok
  does not advertise `type: terminal`; all three rows are agent-typed.
* v2 renames the RPC to `auth/login` and requires `type`. grok 1.0.3
  negotiates `protocolVersion: 1`. Out of scope.

### Implementation

The executable plan is
[0085-PLAN-grok-acp-auth-method-wiring.md](./0085-PLAN-grok-acp-auth-method-wiring.md).
Rationale stays here; steps, gates, and tests live there.

### Assessment (2026-08-13 review)

The first draft's evidence still holds (live initialize catalog,
`authenticate` shapes, `[auth]` / unquoted-table miss, SDK decode).
Three decisions were under-specified for a plan:

| Draft claim | Problem | Locked here |
| --- | --- | --- |
| D2 "only advertised, then skip" | Isolated env-key advertise-only-`grok.com` but `authenticate xai.api_key` works; a static `auth_method_id` pin is still a hang if it is `grok.com` | D2 steps 1 and 4 |
| D4 write every catalog model | Duplicates the secret; user asked for the current default only | D4 resolution 1–4 |
| D7 "fail with grok's `-32000`" | Cold `session/new` sometimes hangs for the full `startTimeout` | D7: do not call `session/new`; do not retain a spare |

`acpagent.New` is called only from `grok.go`. Handshake and fail-fast
can live in `acpagent` without affecting other agents.

### Related

* [0085-PLAN-grok-acp-auth-method-wiring.md](./0085-PLAN-grok-acp-auth-method-wiring.md) — implementation
* [0038](./0038-MADR-grok-acp-parity-assessment.md) — 0.2.112 two-method
  excerpt; superseded for authMethods
* [0072](./0072-MADR-phone-reconnect-and-provider-timeout-incident.md) F9
  — warning classified as noise
* [0074](./0074-MADR-remote-provider-auth-from-phone.md) D1/D3/D19, W2
* [0081](./0081-MADR-grok-1.0.3-surface-parity.md) — 1.0.3 surface; did
  not re-probe authMethods
* [0083](./0083-MADR-provider-auth-activation-and-layout-gaps.md) D2/D4
  — method honesty; grok key-write claim to correct
