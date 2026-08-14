<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Implement treat a phone credential setup as complete only when the agent can actually use it

Associated MADR: [0086-MADR-phone-provider-auth-completion.md](0086-MADR-phone-provider-auth-completion.md)

| field | value |
| --- | --- |
| status | **proposed** 2026-08-14 — for review; no code in this commit |
| phases | P0 isolated xAI authorize probe · P1 wire (`credential_not_accepted`, `host_oauth`) · P2 verify ladder + TTL cache + mutation ring + kilo file fallback (D13) · P3 catalog honesty · P4 status/device read Layer 0 · P5 phone chips, copy, picker · P6 `url_launcher` · P7 grok/codex/goose verify · P8 live pins · P9 errata |
| rule | One commit per phase. Do not push until asked. Each phase leaves daemon and app releasable and interoperable with older phones/daemons. No Go file is staged until `make pre-add-check FILES="…"` is clean. `git commit` without `-m` (prepare-commit-msg hook). |

## Goal

Make the three user requirements true end to end, matching MADR 0086 O2:

1. An API key pasted on the phone is in the store the agent reads, and the
   agent reports the vendor connected — or the phone sees a typed refusal,
   never "Credential saved". Confirm that with the D13 ladder (cheap
   cache / `/config/providers`), not a 4.7 MB `/provider` reread.
2. A provider that can run a turn shows configured and is selectable in the
   model picker, even if other catalog vendors have no key.
3. OAuth the phone can finish opens the system browser and completes; OAuth
   that can only finish on the host is disabled up front; a device poll never
   reports success because the vendor was already configured.

Rationale stays in the MADR. This plan is the execution order, the files,
the algorithms, the tests, and the rollback.

## Scope

**In scope**

* `internal/protocol` — new error code + unavailability reason
* `internal/provider` sentinels; `httpagent`, `kilo`, `opencode`, `credstore`,
  `providerauth`; grok / codex / goose verify-after-write
* `internal/ws` — `writeAuthErr`, `awaitDeviceFlow` log/result
* `apps/mobile` — agent chip, auth error copy, device-flow launcher, tests
* `docs/protocol-v1.md` — register the new code/reason (0036 D4)
* Live-tagged tests behind `live_kilo` / `live_opencode` /
  `MCREMOTE_LIVE_AUTH_WRITE=1`
* Errata notes on 0074 / 0083 claims this record corrects

**Out of scope**

* 0074 W3 / Strategy B reverse callback tunnel
* 0083 D6 goose OS-keyring writes
* 0085 ACP `authenticate` selection and fail-fast (its own plan)
* Engine-side patches (kilo / opencode / grok used as shipped)
* Renaming phone method ids
* Anthropic Pro/Max plugin OAuth (0074 D12)

**Compatibility**

* New wire fields are additive on the existing `provider_auth` capability.
  Old phones ignore unknown `reason` values and unknown error codes (they
  already fall through to `e.message` in `friendlyOpError`).
* Old daemons never emit `credential_not_accepted` / `host_oauth`; new
  phones treat a missing reason as today.

## Grounding facts (verified 2026-08-14)

| # | Fact | Evidence |
| --- | --- | --- |
| G1 | `handleSetCredential` returns `ok` after `SetCredential` returns nil. No connected-set or file re-read. | `internal/ws/server.go:2085-2097` |
| G2 | Kilo/opencode `PUT /auth/{id}` body is `{type:"api", key, metadata?}`. `out` is nil, so a `200 true` with no persist still succeeds. | `kilo/auth.go:126-131`, `opencode/auth.go:213-218`, `httpagent/provider.go:860-907` |
| G3 | `VerifyAPIKeyMethod` no-ops when `methodID == ""` or `methodID == upstreamID+":api"`. Synthesised catalog ids take this branch. | `httpagent/authcatalog.go:251-254` |
| G4 | `BuildCatalog` invents `DefaultAPIKeyMethod` for every vendor not in `GET /provider/auth`. | `authcatalog.go:210-223,240-243` |
| G5 | Live OpenCode `GET /provider/auth` (1.18.17) has 10 vendors and **no** `kilo`. Live kilo `GET /provider/auth` (7.4.21) has 13; `kilo` is oauth-only ("Kilo Gateway (Device Authorization)"); `xai` is SuperGrok oauth, Headless/Remote/VPS oauth, API key. | live GET 2026-08-14 |
| G6 | 2026-08-13 23:43: `provider credential set provider=opencode upstream=kilo` twice. `~/.local/share/opencode/auth.json` gained `kilo:{type:api}`. Live `GET /provider.connected` = `["opencode","opencode-go","openai"]`. | `mcremote.err.log`; live GET |
| G7 | There is no `provider credential set` for the kilo *agent*. `~/.local/share/kilo/auth.json` mtime 2026-08-06. | log + store |
| G8 | Live 2026-08-14 (loopback, running LaunchAgent engines): kilo `GET /provider` 4 944 680 B starts `{"all":[`; `GET /config/providers` 352 660 B; `GET /provider/auth` 3 381 B. OpenCode: 4 812 416 / 123 555 / 2 543 B. No `ETag`, `Last-Modified`, or `Cache-Control` on any of them. `?fields=connected`, `?include=connected`, `?connected=1` still return the full `/provider` body. `HEAD /provider` is 404 (kilo) or 200 HTML (opencode). | live urllib probe |
| G8b | `GET /api/provider` is 96 B (v2 integration list). `GET /api/provider/{id}` is 210–323 B and returns 200 for `togetherai` / `kilo` **whether or not they are in `connected`**. It is not a D1 oracle. | same probe; [v2 Get provider](https://opencode.ai/v2/docs/api/provider/v2-provider-get) |
| G9 | Kilo `AuthStatus` configured set is `GET /config/providers` (or disk if that GET fails). Status upstreams are the 13-method catalog plus extras from that set. | `kilo/auth.go:50-104,179-196` |
| G10 | `worstAuthStatus` ranks `error > quota > missing > configured`. Providers card and detail header use it. Existing widget test expects configured+quota → "Quota reached" (keep). Configured+missing currently becomes "Needs setup". | `provider_status.dart:9-25`; `providers_screen.dart:144`; `provider_detail_screen.dart:163`; `providers_screen_test.dart:40-61` |
| G11 | `awaitEngineCredential` returns on first membership in the configured set. 2026-08-13 18:32 kilo Gateway returned `ok=true` in 5 s because `kilo` was already listed. | `httpagent/deviceauth.go:102-136`; log |
| G12 | `ClassifyCatalogMethod` only maps oauth labels containing `browser` / `external browser` to `oauth_browser`. "Headless / Remote / VPS" becomes `oauth_device`. 2026-08-12 21:00 started `xai` method 1 and finished `ok=false` in 113 s. | `authcatalog.go:177-200`; fixture `kilo/testdata/provider-auth-7.4.20.json:229-241`; log |
| G13 | Device sheet is copy-only. `url_launcher` is not in `apps/mobile/pubspec.yaml`. | `device_flow_sheet.dart:7-10` |
| G14 | Kilo has no `AuthFileWriterDialect`. A down engine cannot accept a phone key. OpenCode can (`SetCredentialFile` → `MergeJSONAuthMetadata`). | `httpagent.go:166-197`; `opencode/auth.go:238-266` |
| G15 | OpenCode has `TestLiveOpenCodeCredentialRoundTrip` (`MCREMOTE_LIVE_AUTH_WRITE=1`) against the **host** store. Kilo has no write live test. | `opencode/live_auth_test.go:105-163`; `kilo/live_auth_test.go` |
| G16 | 0083 error codes already registered: `keyring_managed`, `method_unsupported`, `invalid_key`, `engine_unavailable`, `provider_busy`. New codes must land in `protocol.ErrorCodes()` **and** `docs/protocol-v1.md` or `TestWSErrorCodesAreRegistered` / `TestErrorCodesAreDocumented` fail. `friendlyOpError` has no `credential_not_accepted` case. | `protocol/errors.go:113-138,223-229`; `mc_exception.dart:45-61`; `docs/protocol-v1.md:858+` |
| G17 | `AuthMethod.Unavailable` + `Reason` already exist; `upstreamAuthPayload` copies them, then overlays `browser_only` / `device_unsupported`. A provider-set `host_oauth` is the 0083 D4 extension point. | `provider/auth.go:87-93`; `ws/server.go:1923-1946` |
| G18 | `AuthReason*` constants: `keyring_managed`, `browser_only`, `device_unsupported`. Phone `AuthMethod.reason` is an opaque string; copy for unknown reasons can fall back to the existing host-only chip. | `protocol/messages.go:528-533`; `models.dart:563-576` |
| G19 | `MergeJSONAuthMetadata` is the kilo/opencode file format (0600 atomic). `ReadJSONAuth` returns id+type only. | `credstore/write.go:80-128`; `credstore.go:172-202` |
| G20 | Grok write already targets quoted `[model."<id>"]` and `AuthStatus` reads it (0085 D4/D5 in tree). No phone grok `set_credential` on this host. Codex status is `~/.codex/auth.json` presence. Goose refuses on keyring hosts. | `grok/auth.go`; `codex/auth.go:98-105`; `goose/auth.go:150-182` |

## Decisions index (MADR 0086)

D1 verify-after-write · D2 no synthesised method the engine rejects ·
D3 agent chip = can-run, not worst-missing · D4 status unions the
connected set · D5 device poll waits for a *change* · D6 xAI method 1
host-only until probed · D7 system-browser open · D8 surface failed
device results · D9 kilo file fallback + D1 · D10 model picker uses
the same connected set · D11 grok stays 0085 plus D1/D3 · D12
goose/codex keep 0074/0083 plus D1/D3 · **D13 verify ladder: Layer 0
TTL cache + mutation ring, Layer 1 `/config/providers`, Layer 3
`/provider` only on dispute — never on the happy path**.

## Phase dependency

```text
P0 (probe) ──────────────────────────────────────────────► P3 (catalog)
     │
     ▼
P1 (wire) ─► P2 (D13 ladder + kilo file) ─► P4 (status/device on Layer 0)
                     │                         │
                     ▼                         ▼
              P7 (grok/codex/goose)      P5 (phone chips)
                                               │
                                               ▼
                                          P6 (url_launcher)
                                               │
                                               ▼
                                          P8 (live) ─► P9 (errata)
```

P0 may be a docs-only commit (probe notes in this plan's P0 section) if
the engineer records the URL classification without code. P5 can start
once P1 exists (chips do not need P2). P6 is independent of P2–P4 but
ships after P5 so the sheet rewrite happens once.

---

## Implementation Steps

Gates for every Go phase: `gofmt` clean on edited files; `golint` clean
per file; `make pre-add-check FILES="…"` before `git add`; `go test`
(and `go test -race` before the phase commit) on touched packages.
Dart phases: `dart format` the edited files, `dart analyze`,
`flutter test` for the affected test files. Commit per phase **without**
`-m`. Do not push until asked.

---

### P0 — Isolated xAI authorize probe (MADR D6)

**No production code.** Isolated `kilo serve` only. Do **not** POST
authorize against the operator's LaunchAgent engine on `:52010`.

1. Temp `HOME` / `XDG_DATA_HOME`. Start `kilo serve --hostname 127.0.0.1
   --port <ephemeral>`. Health-check `/global/health`.
2. `POST /provider/xai/oauth/authorize` with `{"method":0}` and again
   with `{"method":1}`. Capture `{url, method, instructions}` only.
   Do **not** complete either flow. Process-kill the engine (pending
   codes expire).
3. Run both URLs through `providerauth.Classify`. Record in this
   plan's P0 results table (fill in when the phase is executed):

| method | label | url shape (redact query secrets) | Classify kind | D6 action |
| --- | --- | --- | --- | --- |
| 0 | SuperGrok Subscription | `https://auth.x.ai/oauth2/authorize?response_type=code&…&redirect_uri=<redacted>&code_challenge=…` (PKCE). Engine `method=auto`. Instructions: "Complete authorization in your browser. This window will close automatically." | **browser** (loopback `redirect_uri`) | `browser_only`. Catalog hint must mark this row unavailable; start-time D7 already refuses it. |
| 1 | Headless / Remote / VPS | `https://accounts.x.ai/oauth2/device?user_code=<redacted>`. Instructions: `Open https://accounts.x.ai/oauth2/device on any device and enter code: XXXX-XXXXX`. Engine `method=auto`. | **device** (RFC 8628 URL + user code) | **Keep offered.** This is a real phone device flow. Do **not** mark `host_oauth`. The 2026-08-12 hang was D5/D7 (no browser open, poll), not a host-only grant. |

Isolated probe 2026-08-14: temp `HOME`/`XDG_*`, `kilo serve --hostname 127.0.0.1 --port 62263`, `POST /provider/xai/oauth/authorize` for methods 0 and 1, engine killed without completing either flow. Operator engine on `:52010` was not called.

4. If method 1 *is* a real RFC 8628 URL+code that the engine completes
   without a host browser, **stop and update MADR 0086 D6** before P3
   — do not smuggle a different classification into the plan.
5. Commit the filled table (this file only) if anything was unknown;
   otherwise fold the result into the P3 commit message body.

P0 filled 2026-08-14. **P3 must not** mark xAI method 1 `host_oauth`.
Mark method 0 (`SuperGrok Subscription`) `browser_only`. `host_oauth`
remains for synthesised kilo-via-opencode sign-in and any later label
that is neither device nor loopback.

---

### P1 — Wire: `credential_not_accepted` and `host_oauth` (MADR D1, D6, D8)

**Files:** `internal/protocol/errors.go`, `internal/protocol/messages.go`,
`internal/protocol/provider_auth_test.go` (if a reason table exists),
`internal/provider/auth.go`, `internal/ws/server.go` (`authErrCode`),
`internal/ws/auth_err_code_test.go`, `docs/protocol-v1.md`,
`apps/mobile/lib/data/ws/mc_exception.dart` (+ its unit test if any).

**Daemon**

```go
// protocol/errors.go
ErrCredentialNotAccepted = "credential_not_accepted"

// protocol/messages.go AuthReason*
AuthReasonHostOAuth = "host_oauth"

// provider/auth.go
var ErrCredentialNotAccepted = errors.New(
    "agent stored the credential but is not using it")
```

Register `ErrCredentialNotAccepted` in `ErrorCodes()`.

`authErrCode` (`server.go:2306-2329`):

```go
case errors.Is(err, provider.ErrCredentialNotAccepted):
    return protocol.ErrCredentialNotAccepted,
        "the agent stored the value but is not using it; this vendor needs a different sign-in method"
```

Add the table row in `auth_err_code_test.go`.

**protocol-v1.md** (next to the 0074/0083 provider-auth paragraph ~858):
document `credential_not_accepted` (write looked like success; agent
did not connect the vendor — pick another method) and reason
`host_oauth` (sign-in finishes on the host, not the phone).

**Phone** `friendlyOpError`:

```dart
case 'credential_not_accepted':
  return 'The host stored that value but the agent is not using it — '
      'this vendor needs a different sign-in, not an API key.';
```

Unknown `reason: host_oauth` on a method: P5 teaches the chip; P1 only
needs the error code so P2 can return it.

**Tests:** Go table; Dart `friendlyOpError` case. Gate + commit.

---

### P2 — Verify ladder, TTL cache, mutation ring, kilo file fallback (MADR D1, D9, D13)

**Files:** new `internal/provider/httpagent/connected.go` (+
`connected_test.go`), `internal/provider/httpagent/httpagent.go`
`SetCredential`/`ClearCredential`/`Provider` fields,
`internal/provider/kilo/auth.go` (+ `auth_test.go`),
`internal/provider/opencode/auth.go` only if it has its own configured
probe to redirect at the cache. Do **not** add a happy-path
`GET /provider`.

#### P2.1 Layer 0 — in-process cache + mutation ring

On `httpagent.Provider` (guarded by the existing `authCatalogMu` or a
dedicated `connectedMu` — do not share the 5-minute catalog pointer
with the 20-second connected set):

```go
type connectedCache struct {
    ids       map[string]struct{}
    gen       uint64            // monotonic; SHA-256 of sorted ids optional
    source    string            // "config" | "provider" | "disk"
    fetchedAt time.Time
    negUntil  map[string]time.Time // negative cache
}

type credMutation struct {
    seq      uint64
    op       string // "set" | "clear" | "device"
    upstream string
    at       time.Time
}

const (
    connectedTTL     = 20 * time.Second
    negativeTTL      = 10 * time.Second
    mutationRingCap  = 32
)
```

Keep `mutations [32]credMutation` as a ring (head index + seq). This
is the ring-buffer the user asked for: it stores *events*, not the
4.7 MB body.

Helpers:

* `snapshot() (ids, gen, seq)` — no I/O
* `fresh() bool` — `time.Since(fetchedAt) < connectedTTL`
* `note(op, upstream)` — append ring, optimistic add/remove on `ids`,
  bump `gen`
* `remember(ids, source)` — replace cache, set `fetchedAt`
* `rememberNegative(id)` — `negUntil[id] = now+negativeTTL`
* `InvalidateConnected()` — expire `fetchedAt` (writes call this
  *and* `note`)

`InvalidateAuthCatalog` stays for the vendor *list*. Writes call both.

#### P2.2 Layer 1 — cheap connected-id fetch

```go
// FetchConfigProviderIDs is GET /config/providers decoded to ids only.
// The struct must not declare Key (MADR 0043 D4).
func FetchConfigProviderIDs(ctx context.Context, api API) (map[string]struct{}, error)
```

Reuse kilo/opencode's existing `connectedProvidersResponse` shape
(`providers[].id` only). 123–353 KB. This is the default confirm.

#### P2.3 Layer 3 — last-resort `/provider` (dispute only)

```go
// FetchProviderConnectedIDs is GET /provider streamed for the
// "connected" field only. It is Layer 3: file-vs-engine disagreement.
// Single-flight: one in-flight call per Provider.
func FetchProviderConnectedIDs(ctx context.Context, api API) (map[string]struct{}, error)
```

Implementation notes (G8):

* The body starts `{"all":[`. `connected` is last. A
  `json.Decoder` + `Token` walk can skip `all` without building the
  model tree (heap), but the 4.7 MB still crosses the socket. That is
  why this is not the default.
* Decode into `struct { Connected []string; Default map[string]string }`.
  Never declare `All` or `Key`.
* `singleflight.Group` keyed `"connected"` on the Provider so five
  status refreshes cannot start five downloads.
* If a future live probe sees an `ETag`, send `If-None-Match` and
  treat 304 as "Layer 0 still valid" (D13 Layer 1.5). Today: no ETag;
  do not invent one on the request.

#### P2.4 Verify helper (the ladder)

```go
func (p *Provider) VerifyUpstreamConnected(ctx context.Context, api API, upstreamID string) error {
    // Layer 1 first — never Layer 3 on the happy path.
    ids, err := FetchConfigProviderIDs(ctx, api)
    if err != nil {
        return fmt.Errorf("verify %s: %w", upstreamID, err)
    }
    p.remember(ids, "config")
    if _, ok := ids[upstreamID]; ok {
        return nil
    }
    // Dispute: file may have the id (23:43). Escalate once.
    full, err := p.fetchProviderConnectedSingleFlight(ctx, api)
    if err != nil {
        return err // maps to engine_unavailable
    }
    p.remember(full, "provider")
    if _, ok := full[upstreamID]; ok {
        return nil
    }
    p.rememberNegative(upstreamID)
    return fmt.Errorf("upstream %s: %w", upstreamID, provider.ErrCredentialNotAccepted)
}
```

Timeout: keep `authWriteTimeout` (20 s). Layer 1 is ~50–200 ms
loopback; Layer 3 is the remaining budget and must be rare.

A test spy on `API` must see `GET /config/providers` on every verify
and `GET /provider` **only** when the fixture omits the id from
`/config/providers`.

#### P2.5 Call it from `httpagent.Provider.SetCredential`

After a successful engine `d.SetCredential` **and** after a successful
file `d.SetCredentialFile` + restart:

1. `p.note("set", upstreamID)` (optimistic Layer 0, not yet `ok`).
2. `VerifyUpstreamConnected`.
3. On success: `remember` already updated Layer 0; return nil.
4. On `ErrCredentialNotAccepted`: **best-effort compensating delete**
   (`ClearCredential` / `ClearCredentialFile`); `note("clear", id)`;
   log warn `credential not accepted` with provider + upstream, no
   secret; return the typed error.
5. On verify transport error: return `engine_unavailable` rather than
   claiming success; do not compensating-delete (the write may be
   fine and the engine only timed out).
6. `InvalidateAuthCatalog` + `InvalidateConnected` as above.

Do **not** verify on `ClearCredential` beyond today's delete. Forget
the id in Layer 0 and append `note("clear", id)`.

`GET /api/provider/{id}` is **not** consulted (G8b).

#### P2.4 Kilo `AuthFileWriterDialect` (D9)

Mirror `opencode/auth.go` `SetCredentialFile` / `ClearCredentialFile`:

* path = `credstore.KiloAuthPath()`
* `MergeJSONAuthMetadata` / `DeleteJSONAuth`
* method-id prefix check (same as OpenCode file path)
* `httpagent.Provider.SetCredential` already falls through to the file
  writer when the engine cannot start

File-path writes still go through P2.4 / P2.5 after
`RestartForCredentialChange`. If the engine still cannot start, verify
cannot run: return the start error, not `ok`. A cold host that cannot
boot `kilo serve` cannot prove connectedness; do not toast success.

#### P2.6 Tests (unit, no live engine)

| test | assert |
| --- | --- |
| `TestFetchConfigProviderIDsDropsKey` | fixture with `providers[].key` → ids only; marshaled result has no secret |
| `TestVerifyHappyPathHitsConfigNotProvider` | PUT 200, `/config/providers` includes togetherai → nil; spy sees **no** `GET /provider` |
| `TestVerifyDisputeEscalatesOnce` | PUT 200, config omits id, `/provider.connected` omits id → `ErrCredentialNotAccepted`; spy sees exactly one `GET /provider`; DELETE issued |
| `TestVerifyDisputeAcceptsIfProviderConnected` | config omits, `/provider.connected` includes → nil (engine accepted; config lagged) |
| `TestConnectedCacheServesWithinTTL` | after `remember`, two `snapshot()` calls and no API; after `connectedTTL+1` a Layer 1 fetch happens |
| `TestNegativeCacheSuppressesReread` | after a failed verify, a second verify inside 10 s does not hit Layer 3 again |
| `TestSingleFlightProviderFetch` | 8 goroutines on expired cache + dispute; exactly one `GET /provider` |
| `TestMutationRingWraps` | 40 `note` calls; ring length 32; seq monotonic |
| `TestSetCredentialFileFallbackWhenEngineDown` | kilo dialect, `ensureServer` fails, file writer writes `auth.json`, restart attempted |
| `TestVerifyDoesNotLogSecret` | log capture during reject; secret substring absent |
| `TestAPIProviderIDIsNotAnOracle` | comment-only / negative: a fake that 200s `/api/provider/togetherai` while omitting it from config **must not** count as connected |

Gate: `make pre-add-check FILES="…"` and
`go test ./internal/provider/httpagent/ ./internal/provider/kilo/ ./internal/provider/opencode/`.
Commit.

---

### P3 — Catalog honesty (MADR D2, D6)

**Files:** `internal/provider/httpagent/authcatalog.go`,
`httpagent/authcatalog_test.go` (or kilo/opencode catalog tests),
`kilo/auth.go` `fetchAuthCatalog` if annotation belongs after fetch,
`ws/method_availability_test.go`.

#### P3.1 Do not synthesise `:api` for denied vendors

```go
// noSyntheticAPIKey is the set of vendor ids that appear in GET
// /provider.all but must not receive DefaultAPIKeyMethod. OpenCode
// 1.18.17 lists `kilo` in `all` and not in /provider/auth; a
// synthesised kilo:api is the 2026-08-13 23:43 write (MADR 0086 D2/F3).
var noSyntheticAPIKey = map[string]struct{}{
    "kilo": {},
}
```

In `BuildCatalog`, when `methods[v.ID]` is missing:

* if `v.ID` ∈ `noSyntheticAPIKey`: attach one disabled method
  `{ID: id+":oauth", Type: AuthMethodOAuthDevice, Label: "Sign in on the host",
  Unavailable: true, Reason: protocol.AuthReasonHostOAuth}` — **not**
  an empty Methods slice (`UpstreamAuth.hasUsableMethod` treats empty
  as usable).
* else: keep `DefaultAPIKeyMethod` (togetherai, deepseek, …).

Kilo-*agent* `kilo` upstream already has a real oauth method from
`GET /provider/auth`; `BuildCatalog` will not synthesise. The deny-list
is for OpenCode's catalog.

#### P3.2 Host-OAuth label markers (D6)

Extend classification **or** post-annotate (prefer post-annotate so
0074 D7 URL classification at start time stays authoritative):

```go
var hostOAuthMarkers = []string{
    "headless", "remote", "vps", "host only", "on the host",
}
```

After `ClassifyCatalogMethod`, if type is `oauth_device` and the label
matches a marker, set `Unavailable=true`, `Reason=AuthReasonHostOAuth`.
This catches kilo `xai:1` ("Headless / Remote / VPS") without waiting
for authorize.

If P0 proved method 1 is a real device code, **delete this marker
match for that label** and update D6 instead.

`xai:0` "SuperGrok Subscription": no "browser" word today, so it is
also `oauth_device`. If P0 shows loopback, the existing D7 URL check
refuses at start (`ErrAuthUnsupported` → "unsupported"). Also
post-annotate if the label should have been browser: add
`"subscription"` only if P0 confirms loopback — do **not** guess.
Until P0, leave `xai:0` as device; start-time D7 still blocks a
loopback URL.

#### P3.3 `VerifyAPIKeyMethod` for `:api`

Keep the empty / `:api` fast path for allow-listed synthesised
methods. Add: if `upstreamID` ∈ `noSyntheticAPIKey` and method is
`:api` or empty, return `ErrAuthMethodUnsupported` **before** PUT.
Belt-and-suspenders with P2 verify.

#### P3.4 Tests

| test | assert |
| --- | --- |
| `TestBuildCatalogDoesNotSynthesiseKiloAPIKey` | vendor `kilo` in `all`, absent from methods → one unavailable `host_oauth` method, no `:api` |
| `TestBuildCatalogStillSynthesisesTogether` | `togetherai` → `:api` usable |
| `TestHostOAuthMarkerDisablesXaiHeadless` | label "xAI Grok OAuth (Headless / Remote / VPS)" → unavailable `host_oauth` |
| `TestVerifyAPIKeyMethodRefusesKiloAPI` | `VerifyAPIKeyMethod(ctx, api, "kilo", "kilo:api")` → `ErrAuthMethodUnsupported` |
| existing kilo `TestSetCredentialCarriesInputsAndGuardsMethod` | still refuses `kilo:0` as oauth |

Phone catalog already renders `available:false` with a reason chip
(0083). P5 adds the `host_oauth` sentence.

Gate + commit.

---

### P4 — Status union, device snapshot, failed-result surfacing (MADR D4, D5, D8)

**Files:** `kilo/auth.go`, `opencode/auth.go`,
`httpagent/deviceauth.go`, `kilo/device_auth.go`,
`opencode/device_auth.go`, `httpagent/deviceauth` tests,
`kilo/device_auth_test.go`, `opencode/device_auth_test.go`,
`internal/ws/server.go` `awaitDeviceFlow`,
`internal/provider/credstore` (fingerprint helper).

#### P4.1 Status configured set (D4)

Do **not** `GET /provider` on every `providers.list` (G8, D13).

Configured ids = Layer 0 snapshot, refreshed thus:

1. If `fresh()`, return cached ids (0 B).
2. Else Layer 1: `GET /config/providers` ids ∪ env-keyed ids
   (OpenCode `envUpstreams`). `remember(..., "config")`.
3. Disk `auth.json` ids join the set **only** when the engine is down
   (`live=false`). While the engine is up, disk-only ids stay out —
   that is the 23:43 guard. After a D1-accepted write the id is
   already in Layer 0 from `note` + Layer 1 confirm, so the detail
   list grows without Layer 3.

Kilo `AuthStatus` unions the methods catalog ∪ this set. OpenCode
`engineAuthStatus` uses the same snapshot instead of a private
`connectedProviders` GET every time.

`state.Status = AuthConfigured` iff the set is non-empty (already
true). D3 is the phone chip; leave daemon `Status` as is.

**Tests:** after `note("set","togetherai")` + `remember` including
it, `AuthStatus` contains togetherai and the API spy sees no
`/provider`. Disk-only kilo + live engine + not in Layer 0 → **not**
configured.

#### P4.2 Device completion = change, not membership (D5)

Replace `awaitEngineCredential`'s membership check.

Before `POST …/oauth/authorize` (in `StartEngineDeviceFlow`, after
method index is known):

```go
before := p.snapshot() // Layer 0 gen + mutation seq + present?
fp := fingerprintDisk(upstreamID) // type + expires, no key
```

Wait loop (every `devicePollInterval`):

1. If `p.seq` / `p.gen` unchanged **and** cache `fresh()`, do not
   hit the network (D13 Layer 0).
2. Else refresh Layer 1 (not Layer 3).
3. Succeed only if the upstream is newly present **or**
   `fingerprintDisk` changed (expires/type). Membership that was
   already true with an unchanged fingerprint is **not** success
   (the 18:32 false complete).

Timeout / cancel unchanged. If the engine is down (`!live`), keep
waiting (today's behaviour). Do not call `GET /provider` from the
poll loop.

Kilo Gateway already-configured case: `before.present==true` and
`expires` unchanged → do **not** return ok on the first tick.

**Compensating:** if authorize starts and wait fails, do not delete
an *old* credential (D5 must not destroy a working Gateway session).

**Tests:** rewrite `kilo/device_auth_test.go` / opencode equivalent:

| test | assert |
| --- | --- |
| already in set, expires unchanged | wait does not return nil on first poll; cancel → ctx.Err |
| already in set, expires changes | wait returns nil |
| id newly appears | wait returns nil |
| never appears, fake clock / short deadline | timeout error |

Drive the ticker by injecting a clock or by making
`devicePollInterval` a var for tests (if not already). Prefer a
`poll` hook over sleeping 5 s in unit tests.

#### P4.3 Failed device result (D8)

`awaitDeviceFlow` (`server.go:2224-2248`):

* On error, set `payload.Error = clipAuthErr(err.Error())` (already)
  and `payload.ErrorKind` (already).
* Log at info: `err` clipped, `error_kind`, `ok=false` — today only
  `ok` is logged, which is why 21:00 / 12:34 are unexplained.
* Phone already maps `ok != true` to the error string on the sheet
  (`provider_detail_screen.dart:550-554`). Confirm the result stream
  is still routed (`mcremote_client.dart` `oauth.device_flow_result`).
  Add a widget test that a completed `result` future with a string
  shows `Sign-in failed: …` and stops the spinner.

Cancel: dismissing the sheet already calls `onCancel` →
`cancelDeviceAuth`. Keep that; add a test that cancel is invoked when
the sheet is dismissed before `_done`.

Gate: `go test ./internal/provider/httpagent/ ./internal/provider/kilo/
./internal/provider/opencode/ ./internal/ws/`; Flutter device-sheet
test. Commit.

---

### P5 — Phone chips, copy, picker cache (MADR D3, D6, D10)

**Files:** `apps/mobile/lib/features/settings/provider_status.dart`,
`providers_screen.dart`, `provider_detail_screen.dart`,
`upstream_catalog_sheet.dart`, `provider_auth_sheet.dart`,
`apps/mobile/lib/data/protocol/models.dart` (reason copy if needed),
`apps/mobile/test/providers_screen_test.dart`,
`provider_detail_screen_test.dart`, catalog/auth-sheet tests,
`apps/mobile/lib/features/widgets/model_picker_sheet.dart` only if
copy changes.

#### P5.1 Agent chip (D3)

```dart
String agentAuthStatus(ProviderAuthInfo auth) {
  // error / quota still dominate (0082 D4, existing quota test).
  for (final up in auth.upstreams) {
    if (up.status == AuthStatus.error) return AuthStatus.error;
    // track quota separately
  }
  if (auth.upstreams.any((u) => u.status == AuthStatus.quota)) {
    return AuthStatus.quota;
  }
  if (auth.status == AuthStatus.configured ||
      auth.upstreams.any((u) => u.isConfigured)) {
    return AuthStatus.configured;
  }
  return AuthStatus.missing;
}
```

Keep `worstAuthStatus` as a wrapper that calls `agentAuthStatus` **or**
replace call sites and delete the old ranking for `missing`.
`firstAuthAnomaly` already ignores `missing`; leave it.

Call sites: `providers_screen.dart:144`,
`provider_detail_screen.dart:163` (header only). Per-row chips stay
`StatusChip.auth(up.status)`.

**Tests:**

* Existing configured+quota → "Quota reached" **must stay green**.
* **New:** configured together + missing deepseek → card shows
  "Configured" and "1 credential", **not** "Needs setup".
* Detail header matches the card; the deepseek row still says
  "Needs setup".

#### P5.2 `host_oauth` copy

Catalog / method dropdown / browser notice:

* `reason == host_oauth` → "This sign-in finishes on the host, not
  the phone."
* Keep `browser_only` copy as today.

Auth sheet: `isUsable` is already `available && !isBrowserOAuth`.
Daemon-sent `available:false` covers `host_oauth`. Default method
stays first usable (0083 D4) — xAI will default to the API-key
method once method 1 is disabled. Add a widget test: xAI methods
[headless unavailable, api usable] → default is api, Save enabled.

#### P5.3 Model picker (D10)

No extra `/provider`. Confirm `ListModelProvidersLive` reads
Layer 0: after `note("set","togetherai")` + Layer 1 confirm, the
picker marks that id connected even if the 5-minute vendor-list
cache is still warm. If the picker still does its own
`connectedProviders()` GET, point it at `p.snapshot()` first and
Layer 1 only on miss.

Empty-state copy can stay; it becomes true once D1/D4 work.

Gate: `dart format` + `flutter test` on the listed files. Commit.

---

### P6 — Open device URLs in the system browser (MADR D7)

**Files:** `apps/mobile/pubspec.yaml`,
`apps/mobile/lib/features/settings/device_flow_sheet.dart`,
`apps/mobile/test/device_flow_sheet_test.dart` (create if missing),
Android / iOS platform manifests if the plugin requires them.

1. `flutter pub add url_launcher` in `apps/mobile`.
2. Device sheet: the URI becomes a `TextButton` / `InkWell`
   `key: Key('device-flow-open-uri')` that calls
   `launchUrl(Uri.parse(uri), mode: LaunchMode.externalApplication)`.
   On failure, a SnackBar "Could not open the link" and leave copy
   available.
3. Keep copy-link and copy-code.
4. Update the file doc comment: delete "url_launcher is not yet a
   dependency".
5. Android: `url_launcher` 6.x uses the plugin's own queries; confirm
   `AndroidManifest.xml` does not need a manual `<queries>` for https.
   iOS: no extra plist for https `externalApplication`.
6. Widget test: tap open-uri with a fake launcher (inject
   `Future<bool> Function(Uri)` so tests do not hit the platform
   channel). Assert the fake saw the flow's URI.

This is **not** Strategy B. Loopback rows stay disabled.

Gate: `flutter test` device-flow + analyze. Commit.

---

### P7 — Grok / Codex / Goose verify-after-write (MADR D11, D12)

**Files:** `internal/provider/grok/auth.go` (+ tests),
`internal/provider/acpagent/acpagent.go` `SetCredential` (after the
spec write), `internal/provider/codex/auth.go` (+ tests),
`internal/provider/goose/auth.go` (+ tests).

Do **not** implement 0085's ACP handshake here.

| agent | after native write, success iff | on failure |
| --- | --- | --- |
| grok | `HasGrokConfigAPIKey(path)` true (quoted table or leftover `[auth]`) | `ErrCredentialNotAccepted`; do not leave a half-written table — `ClearGrokModelAPIKey` the target id |
| codex | `credstore.FileExists(CodexAuthPath())` | same sentinel; `codex logout` is too aggressive on a failed key write — only fail the RPC |
| goose | secret name present in `ReadGooseSecretNames` **and** keyring still disabled | existing `ErrGooseKeyringManaged` path unchanged; missing name after write → `ErrCredentialNotAccepted` |

Grok `SetCredential` already requires `inputs["model"]` via
`fillCredentialModel`. If that fails, the write never happens (already
an error). P7 only covers "write returned nil but presence is false".

**Tests:** temp files; write then flip/delete the file before verify
by splitting the helper so tests can inject a failing presence check.
Do not call live `codex login` in unit tests (existing stdin tests
stay).

Gate: pre-add-check on the three packages. Commit.

---

### P8 — Live pins (MADR confirmation 1, 2, 3, 6, 8)

**Files:** `internal/provider/kilo/live_auth_test.go` (extend),
`internal/provider/opencode/live_auth_test.go` (extend), optionally
`internal/provider/httpagent` if the verify helper needs a live
smoke.

**Hard rule:** new write tests isolate `HOME` + `XDG_DATA_HOME` +
`XDG_CONFIG_HOME` to `t.TempDir()` and boot their own `kilo serve` /
`opencode serve` via the provider's `Start` / `ensureServer`. Do
**not** extend `TestLiveOpenCodeCredentialRoundTrip`'s habit of
touching the operator's real `auth.json` for the *new* cases. Leave
the old test as-is (it is already gated).

| test | tag / env | pass if |
| --- | --- | --- |
| `TestLiveKiloTogetherAIKeyRoundTrip` | `live_kilo` + `MCREMOTE_LIVE_AUTH_WRITE=1` | isolated engine: Set togetherai `:api` scratch → Layer 1 ids contain it **and** isolated `auth.json` has the id; API log of the isolated server (or a wrapping spy) shows **no** `GET /provider` on that write; Clear removes both |
| `TestLiveKiloXaiAPIKeyRoundTrip` | same | `xai` method `:2` or `:api` (whichever the live catalog assigns the api row) writes and connects; Clear restores |
| `TestLiveOpenCodeRefusesKiloAPIKey` | `live_opencode` + write env | `SetCredential("kilo","kilo:api", scratch)` returns `ErrAuthMethodUnsupported` **or** `ErrCredentialNotAccepted`; isolated `auth.json` has no `kilo` (or the compensating delete removed it); `connected` unchanged |
| `TestLiveKiloCatalogMarksXaiHeadlessUnavailable` | `live_kilo` (read-only) | catalog/status xAI methods: Headless/VPS `Unavailable` + `host_oauth`; API key usable |
| `TestLiveKiloGatewayDeviceDoesNotFalseComplete` | `live_kilo` | if isolated home has no kilo oauth, skip; else start Gateway device, cancel before any user action, assert wait does not return nil inside 2× poll interval |

Run once at acceptance, not in a loop. Tokens cost nothing here except
process spawn time; still do not loop.

Existing OpenCode togetherai round-trip should now **fail** if the
engine accepts PUT but omits togetherai from `connected` — that is D1
working. If that test starts failing on this host, that is a signal,
not a reason to weaken verify.

Gate: non-live tests green; live run recorded in the commit body by
the engineer with the CLIs. Commit the test code even if live is
deferred to acceptance (LookPath / env skip).

---

### P9 — Errata (MADR F14)

Comment and one-line pointers only. Do not rewrite historical
narrative.

* `docs/0074-MADR-remote-provider-auth-from-phone.md` §14.4 / W1
  acceptance: add "corrected by 0086: engine 2xx is not completion;
  kilo-via-opencode `:api` is not a real method".
* `docs/0083-MADR-provider-auth-activation-and-layout-gaps.md` A8 /
  activation matrix: kilo/opencode "plain API key ✅" becomes "✅ only
  after connected-set verify (0086 D1); synthesised kilo:api ❌".
* `docs/0086-MADR-phone-provider-auth-completion.md`: set
  `status: accepted` only when the owner accepts; this phase does not
  flip it.
* `httpagent.go:145-148` comment still says "Kilo is the only one
  today" for `AuthWriterDialect` and that OpenCode writes files
  instead — both stale since 0083. Point at engine PUT + D1 verify.
* `kilo/auth.go:107-111` comment "no file poking" — kilo now has a
  file fallback (P2.4).

No behaviour change. Commit.

---

## Verification

Cross-phase acceptance after P8; maps to MADR Confirmation.

| # | MADR | How |
| --- | --- | --- |
| 1 | Isolated kilo togetherai write, no `/provider` | P8 + P2 spy |
| 10 | Dispute path is the only `/provider` | P2 `TestVerifyDisputeEscalatesOnce` |
| 2 | Isolated OpenCode kilo:api refused | P8 `TestLiveOpenCodeRefusesKiloAPIKey` |
| 3 | Gateway device no false complete | P4 unit + P8 skip-or-assert |
| 4 | Providers card: 2 live + 11 missing → Configured | P5 widget test |
| 5 | Detail list + model picker show the new vendor | P4 unit + P5/D10; live smoke on device |
| 6 | xAI method 0 `browser_only`; method 1 usable device; method 2 writes | P3 unit + P8 catalog + key tests |
| 7 | Device URL opens; failed result visible | P6 widget + P4.3 widget |
| 8 | Live tags fail on drift | P8 |
| 9 | No "credential set" info log unless verify passed | P2 (log sits after SetCredential) |

**Manual smoke (real Android phone, this host, after a daemon restart
that includes P1–P7):**

1. Settings → Providers → kilo card is Configured (Gateway +
   opencode-go already live), not Needs setup.
2. kilo → Add credential → togetherai → paste a **real** key if
   available, else skip live turn and use the isolated test. Expect
   either Configured togetherai row + picker Connected, or a red
   toast `credential_not_accepted` — never a green toast with no row.
3. OpenCode → kilo vendor: no API-key Save; host-only chip.
4. kilo → xAI: Headless/VPS disabled; API key offered.
5. kilo Gateway *Start sign-in*: sheet shows URL; tap opens Chrome
   Custom Tab / system browser; if already signed in, sheet does
   **not** flip to "Signed in" in 5 s without a token change.
6. Grok → xAI API key: after save, `~/.grok/config.toml` has exactly
   one quoted `[model."…"] api_key`; card configured. (0085 path +
   P7.)

**Commands**

```bash
make pre-add-check
go test -race ./internal/provider/... ./internal/ws/ ./internal/protocol/
cd apps/mobile && dart format --output=none --set-exit-if-changed . \
  && dart analyze && flutter test
# acceptance only:
go test -tags live_kilo ./internal/provider/kilo/ -run 'LiveKilo' -count=1 -v
MCREMOTE_LIVE_AUTH_WRITE=1 go test -tags live_opencode \
  ./internal/provider/opencode/ -run 'LiveOpenCodeRefuse|LiveOpenCodeAuth' -count=1 -v
```

## Rollout and Rollback

**Rollout**

* Ship daemon first (P1–P4, P7). Old phones: new error code displays
  as the daemon message string; `host_oauth` methods look like
  today's disabled-with-reason rows if the phone already understands
  `available:false` (0083). Phones older than 0083 ignore `available`
  and may still tap Headless/VPS — start-time D7 / P4.2 then fail
  honestly instead of false-succeeding.
* Then ship the app (P5–P6) so chips and the browser button match.
* Restart `mcremote` after the daemon build. Engines respawn; no
  credential migration. Optionally delete the dead
  `~/.local/share/opencode/auth.json` `kilo` api entry from 23:43
  (operator action; not automated — D2 forbids a daemon sweep of
  secrets it cannot classify safely).

**Rollback**

* Each phase is independently revertable by reverting its commit.
* P1 is additive; reverting it without reverting P2 would turn
  `ErrCredentialNotAccepted` into residual `credential_failed` (still
  safe, worse copy).
* P2 compensating delete: if a legitimate write is mis-verified, the
  user must paste again. Rollback P2 restores "toast on 2xx".
* P3 deny-list: rollback restores the fake kilo:api row.
* P6: remove the dependency with `flutter pub remove url_launcher` if
  the plugin misbehaves; copy-only is the previous behaviour.
* No schema migration; no on-disk format change except kilo's new
  ability to merge `auth.json` the same way OpenCode already does.

**Risk register**

| risk | mitigation |
| --- | --- |
| Happy-path write accidentally calls `/provider` | P2 spy test fails the phase; D13 Layer 1 is the only default |
| Layer 1 (`/config/providers`) lags one tick after PUT | one retry of Layer 1 inside the 20 s write timeout (100–200 ms apart) before escalating to Layer 3 |
| Layer 3 4.7 MB on a dispute | single-flight; stream-decode `connected` only; at most once per verify |
| TTL serves a stale "not connected" after a successful write | writes `note` + invalidate immediately; TTL is for *reads*, not for D1 |
| Ring buffer of the 4.7 MB body | **not implemented**; the mutation ring is 32 events |
| Engine grows an ETag later | Layer 1.5: send `If-None-Match`; 304 = Layer 0 still valid; pin with a live test |
| Bloom filter "optimisation" | rejected in D13; exact map only |
| P0 shows method 1 is a real device flow | update MADR D6 before P3; do not ship `host_oauth` on a working method |
| Compensating delete races a human `kilo auth login` | last-writer-wins already (0074 D10); only delete the id we just wrote |
| `url_launcher` store listing / Play policy | https-only, `externalApplication`; no custom scheme |
| Live write test hits the real store | P8 isolates XDG; old OpenCode test stays opt-in and documented |

## More Information

Implementation does not start until the owner accepts MADR 0086 (and
this plan). If a phase reveals that D6's default (`host_oauth` on xAI
method 1) is wrong, update the MADR first.

Related: [0074](./0074-MADR-remote-provider-auth-from-phone.md),
[0083](./0083-MADR-provider-auth-activation-and-layout-gaps.md),
[0085](./0085-MADR-grok-acp-auth-method-wiring.md) /
[0085-PLAN](./0085-PLAN-grok-acp-auth-method-wiring.md),
[0075](./0075-MADR-kilo-cli-provider.md).
