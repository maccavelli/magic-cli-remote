<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Implement grok ACP auth-method wiring

Associated MADR: [0085-MADR-grok-acp-auth-method-wiring.md](0085-MADR-grok-acp-auth-method-wiring.md)

| field | value |
| --- | --- |
| status | **proposed** 2026-08-13 — for review; no code in this commit |
| phases | P1 credstore primitive · P2 grok write/status · P3 ACP handshake + fail-fast · P4 catalog `xai:browser` · P5 live_grok pins · P6 comment/errata |
| rule | One commit per phase. Do not push until asked. Each phase leaves daemon and app releasable. `acpagent.New` is grok-only (`grok.go:102`); handshake changes may live in `acpagent` if gated on spec fields that only grok sets. |

**Accepted credential-lifecycle follow-up:** [MADR 0074 §15](0074-MADR-remote-provider-auth-from-phone.md)
and [0074-PLAN P17–P22](0074-PLAN-remote-provider-auth-from-phone.md) preserve
this plan's ACP authentication and quoted per-model API-key decisions, while
adding isolated Grok device login, OAuth generation recovery, method-specific
logout, and serialization between `config.toml` and `auth.json` mutations.

## Goal

Make grok's advertised ACP auth methods and the phone's grok credential
flows the same truth: the daemon authenticates with a headless-safe
method (or fails fast), and a phone-pasted xAI key is written under
the **current default** model table that grok 1.0.3 actually reads.

## Scope

**In scope**

* `internal/provider/credstore` grok TOML write/clear/presence
* `internal/provider/grok` `setCredential` / `clearCredential` / `authStatus`
* `internal/provider/acpagent` initialize handshake, prewarm, `Start`
* Grok auth catalog row `xai:browser` (type `oauth_browser`)
* Unit tests + `live_grok` pins
* Comment fixes that currently say grok needs no ACP auth

**Out of scope**

* ACP v2 `auth/login` / `auth/logout` (grok 1.0.3 speaks v1; logout is
  `-32601`)
* Strategy B / loopback tunnel for `grok.com` (0074 W3)
* Surfacing `authenticate` `_meta` (email, tier) on the phone
* Renaming phone method ids `xai:api` / `xai:device`
* Writing the key under every catalogued model
* Injecting `XAI_API_KEY` into the LaunchAgent
* Goose / other future `acpagent` users (none exist; keep new behaviour
  behind grok-set spec fields anyway)

## Grounding facts (verified 2026-08-13)

| # | Fact | Evidence |
| --- | --- | --- |
| G1 | Grok 1.0.3 `initialize.authMethods` is credential-gated. LaunchAgent (auth.json, no `XAI_API_KEY`): `cached_token` + `grok.com`, `defaultAuthMethodId=cached_token`. Cold isolated home: `grok.com` only. Cold + quoted `[model."grok-4.5"] api_key`: `xai.api_key` + `grok.com`, default `xai.api_key`. Host + env key: all three, default `cached_token`. | MADR 0085 D1 table; live stdio probe |
| G2 | Methods have no `type`. `acp-go-sdk v0.13.5` decodes each as `AuthMethod.Agent`. SDK unmarshal of a 3-method initialize is `len=3`. Daemon `count=2` is the LaunchAgent process, not an SDK drop. | `types_gen.go:477-561`; `/tmp/grok-acp-init.json` |
| G3 | `authenticate` takes only `{methodId}`. `cached_token` / logged-in `grok.com` return identity `_meta` in <2s. Cold `grok.com` does not return in 8s. Unknown id: `-32602 unsupported auth method`. ACP `logout`: `-32601`. | ACP v1 schema; live probe |
| G4 | Cold + `XAI_API_KEY` advertised only `grok.com`, but `authenticate xai.api_key` returned `{}` and `session/new` succeeded. D2 step 4 exists for this. | isolated-home probe |
| G5 | Cold `session/new` with no authenticate timed out at 15s. After a failed `authenticate xai.api_key` it returned `-32000` / `no auth method id provided`. Hang vs error is not stable. | isolated-home probe; `startTimeout=30s` (`acpagent.go:33`) |
| G6 | `[auth] api_key` and unquoted `[model.grok-4.5]` do not satisfy grok. `inspect` on the latter: `unknown-field` `target=model` `key=grok-4` `field=5`. Quoted `[model."grok-4.5"]` works. | `credstore/write.go:321-341`; inspect probe |
| G7 | Live `currentModelId` is `grok-4.6` on this SuperGrok host and `grok-4.5` on a cold isolated home. `ListModels` already exposes that as `DefaultIDs[0]` after initialize (`modelsToCatalog`, `withCatalogDefault`). | `acpagent.go:248-292,902-952`; live initialize |
| G8 | `SetCredential` / `ClearCredential` on `acpagent.Provider` delegate to `Spec` (`acpagent.go:190-205`). `setCredential` is a package func with no `*Provider`, so model resolution cannot live there without a new spec hook or `inputs["model"]`. | cited lines |
| G9 | `acpagent.New` is called only from `grok.go:102,107`. | repo grep |
| G10 | Handshake today: if `len(AuthMethods)>0 && AuthMethodID!=""` → `conn.Authenticate`; else if methods > 0 → warn (`acpagent.go:456-472`). Host config has `auth_method_id: ""`. | `~/.config/mcremote/config.yaml:69` |
| G11 | `EnsureWarm` stores whatever `spawnAgent` returns (`acpagent.go:584-596`). `Start` claims it and always calls `session/new` (`:786`). A spare that cannot authenticate will hang the first create. | cited lines |
| G12 | `upstreamAuthPayload` already marks `type==oauth_browser` as `available:false` / `browser_only` (`ws/server.go:1937-1938`). Phone `AuthMethod.isUsable` is `available && !isBrowserOAuth` (`models.dart:576`). Adding a grok browser method needs **no** phone renderer change. | cited lines |
| G13 | `authStatus` is configured iff `XAI_API_KEY` or `auth.json` exists (`grok/auth.go:70-75`). It never reads `config.toml`. | cited lines |
| G14 | Existing credstore tests (`write_test.go:238-287`) assert `[auth]` write/replace/clear. They must change with the primitive. | cited lines |
| G15 | Live helper `startACP` (`live_helpers_test.go:17-80`) already speaks initialize + session/new against grok 1.0.3 the same way the daemon does. | cited file |

## Decisions index (MADR 0085)

D1 live catalog, never pin a grok default id · D2 headless-safe
`authenticate` (override → `cached_token` → advertised `xai.api_key`
→ env/file `xai.api_key` → skip) · D3 `grok.com` host-only · D4 write
**current default** quoted `[model."<id>"]` only · D5 status observes
file key presence · D6 keep `xai:api` / `xai:device`; add
`xai:browser` · D7 no `session/new` / no spare when D2 selects
nothing.

## Implementation Steps

Gates for every phase that touches Go: `gofmt` clean on edited files,
`golint` clean on edited files, `go test` (and `go test -race` before
the phase commit) for the packages touched. Repo rule: **no Go file is
staged until `make pre-add-check FILES="…"` is clean.** Dart format
if P4 forces a fixture change. Commit per phase **without** `-m`
(prepare-commit-msg hook). Do not push until asked.

---

### P1 — credstore primitive (MADR D4 store)

**Files:** `internal/provider/credstore/write.go`,
`internal/provider/credstore/write_test.go`. Optionally
`internal/provider/credstore/credstore.go` if presence lives there.

**Change the signatures. Do not keep a `[auth]`-writing overload.**

```go
// SetGrokModelAPIKey writes api_key under exactly one quoted table
// [model."<modelID>"] (MADR 0085 D4). It also deletes a leftover
// [auth] api_key written by the previous implementation.
func SetGrokModelAPIKey(path, modelID, key string) error

// ClearGrokModelAPIKey removes api_key from [model."<modelID>"] and
// from leftover [auth]. Other model tables are left alone.
func ClearGrokModelAPIKey(path, modelID string) error

// HasGrokConfigAPIKey reports whether any quoted [model."…"] table or
// leftover [auth] table contains an api_key line. It must not return
// or log the value (0074 D2).
func HasGrokConfigAPIKey(path string) bool
```

**Deterministic write algorithm**

1. `ValidateSecret(key)`. If `strings.TrimSpace(modelID)==""`, return
   `fmt.Errorf("grok model id is empty")`.
2. Read path; missing file is an empty line slice (same as today).
3. Table header is exactly `[model."` + tomlBasicEscape(modelID) + `"]`
   where `tomlBasicEscape` is the existing `escapeTOML` (backslash and
   quote). Do **not** emit unquoted `[model.grok-4.5]`.
4. Walk lines. Track `inTarget` / `inAuth` by trimmed section headers.
5. If `inTarget` and line is an `api_key` assignment, replace with
   `api_key = "<escaped>"` (one replacement).
6. If `inAuth` and line is an `api_key` assignment, **drop** the line
   (migration).
7. If no replacement happened, append `\n` + header + `api_key` line.
8. `writeFileAtomic(…, 0o600)`.

**Deterministic clear algorithm**

1. Missing file → nil.
2. Drop `api_key` lines while `inTarget` or `inAuth`. Leave empty
   `[auth]` / `[model."…"]` headers in place (do not rewrite the rest
   of the file).
3. Atomic 0600 write.

**Deterministic presence**

True iff any `api_key` assignment is seen while in a header matching
`[auth]` or `[model."…"]` (quoted). Do not treat `[model.grok-4.5]`
(unquoted, broken) as presence — grok does not honour it (G6).

**Tests** (`write_test.go`; replace `TestSetAndClearGrokAPIKey` and
`TestSetGrokAPIKeyEscapesValue`)

| test | assert |
| --- | --- |
| `TestSetGrokModelAPIKeyWritesQuotedTable` | `Set(path, "grok-4.6", "xai-secret-1")` produces `[model."grok-4.6"]` + `api_key = "xai-secret-1"` and **no** `[auth]` |
| `TestSetGrokModelAPIKeyRejectsEmptyModel` | empty / whitespace modelID errors; file not created |
| `TestSetGrokModelAPIKeyReplacesSameModel` | second write, same id: exactly one `api_key`, new value |
| `TestSetGrokModelAPIKeyDoesNotTouchOtherModel` | pre-existing `[model."grok-4.5"] api_key` survives a write to `grok-4.6` |
| `TestSetGrokModelAPIKeyMigratesLegacyAuthTable` | file starting with `[auth]\napi_key = "old"` ends with no `[auth]` key and a quoted default table |
| `TestSetGrokModelAPIKeyEscapesValue` | `we"ird\key` → `api_key = "we\"ird\\key"` |
| `TestSetGrokModelAPIKeyQuotesDottedID` | model `grok-4.5` → header `[model."grok-4.5"]`, never `[model.grok-4.5]` |
| `TestClearGrokModelAPIKeyRemovesTargetAndLegacy` | clears the named table's key and leftover `[auth]`; leaves a different model's key |
| `TestHasGrokConfigAPIKey` | true for quoted table; true for leftover `[auth]`; false for unquoted `[model.grok-4.5]`; false for empty file |
| `TestHasGrokConfigAPIKeyDoesNotContainSecret` | if the test logs, the log must not contain the key value |

Gate: `make pre-add-check FILES="internal/provider/credstore/write.go internal/provider/credstore/write_test.go"`
and `go test ./internal/provider/credstore/`. Commit.

---

### P2 — grok write/status use the primitive (MADR D4, D5, D6)

**Files:** `internal/provider/grok/auth.go`, `auth_test.go`,
`internal/provider/acpagent/acpagent.go` (spec hook +
`SetCredential`/`ClearCredential` wrappers), `acpagent/config.go` only
if a comment must move.

**Spec hook** (add to `acpagent.Spec`, nil-safe):

```go
// CredentialModel resolves the single model id a store-keyed write
// should target (MADR 0085 D4). Called by Provider.SetCredential and
// ClearCredential. list is Provider.ListModels; cfgModel is Config.Model.
CredentialModel func(ctx context.Context, list func(context.Context) (picker.Catalog, error), cfgModel string) (string, error)
```

**`acpagent.Provider.SetCredential`** (`acpagent.go:190`):

1. If `spec.SetCredential == nil` → `ErrAuthUnsupported` (unchanged).
2. Clone `inputs` (nil → empty map) so we never mutate the caller's map.
3. If `spec.CredentialModel != nil`: `id, err := spec.CredentialModel(ctx, p.ListModels, p.cfg.Model)`; on error return it; set `inputs["model"]=id`.
4. Call `spec.SetCredential(ctx, upstreamID, methodID, secret, inputs)`.

**`acpagent.Provider.ClearCredential`:**

1. Resolve `id` the same way when the hook is set.
2. Change `Spec.ClearCredential` to
   `func(ctx context.Context, upstreamID, modelID string) error`.
   Only grok assigns this field today.

**`grok.resolveCredentialModel`** (new, table-tested):

```
if strings.TrimSpace(cfgModel) != "" → return that          // D4.1
if list != nil:
    cat, err := list(ctx)
    if err == nil:
        if len(cat.DefaultIDs)>0 && cat.DefaultIDs[0] != "" → return that  // D4.2
        if len(cat.Options)>0 && cat.Options[0].ID != "" → return that     // D4.3
return "", fmt.Errorf("grok: no default model for key write")              // D4.4
```

Do **not** hard-code `grok-4.6` in this function. The static floor
already supplies `Options[0]` via `ListModels` → `staticModelCatalog`
when harvest fails. Wiring:

```go
// grok.go spec literal
CredentialModel: resolveCredentialModel,
```

**`grok.setCredential`:** keep the existing upstream / method / secret
guards. Then:

```go
modelID := strings.TrimSpace(inputs["model"])
if modelID == "" {
    return fmt.Errorf("grok: missing model for key write")
}
return credstore.SetGrokModelAPIKey(path, modelID, secret)
```

**`grok.clearCredential`:** `return credstore.ClearGrokModelAPIKey(path, modelID)`.

**`grok.authStatus`:** `configured` if any of:

1. `strings.TrimSpace(os.Getenv("XAI_API_KEY")) != ""`
2. `credstore.FileExists(GrokAuthPath())`
3. `credstore.HasGrokConfigAPIKey(GrokConfigPath())`

Still never read the key value. Methods list: keep `xai:api` and
`xai:device`; **do not** add `xai:browser` here — that is P4 so P2
stays a store/status commit.

**Unit tests**

* `TestSetCredentialGuardsMethodID` — still refuses `xai:device` /
  foreign ids (`auth_test.go`).
* `TestSetCredentialRequiresModelInput` — empty inputs → error, no
  file write (`t.Setenv("HOME", tmp)`).
* `TestSetCredentialWritesQuotedDefault` — `inputs{"model":"grok-4.5"}`
  writes `[model."grok-4.5"]`.
* `TestResolveCredentialModelOrder` — table: cfg wins; else
  `DefaultIDs[0]`; else `Options[0]`; else error. Fake `list` closures.
* `TestAuthStatusSeesConfigKey` — isolated HOME, write a quoted table,
  no env, no auth.json → `AuthConfigured`.
* `TestAuthStatusStillSeesAuthJSON` / env — existing behaviour pinned.
* `acpagent` test: `Provider.SetCredential` with a `CredentialModel`
  hook puts `inputs["model"]` before calling `SetCredential`.

Gate: pre-add-check on every touched Go file; `go test ./internal/provider/grok/ ./internal/provider/acpagent/ ./internal/provider/credstore/`. Commit.

---

### P3 — ACP handshake and fail-fast (MADR D1, D2, D7)

**Files:** `internal/provider/acpagent/acpagent.go`, new
`internal/provider/acpagent/authselect.go` (pure function + tests),
`session` struct in `session.go` or `acpagent.go`,
`internal/provider/grok/grok.go` (spec fields),
`internal/config/config.go` (comment only, or defer to P6).

**Spec fields** (nil / empty = today's AuthMethodID-only behaviour):

```go
// SafeAuthMethodIDs are ACP authenticate ids this daemon may invoke
// without an operator pin (MADR 0085 D2). Empty → do not auto-select.
SafeAuthMethodIDs []string

// HasHeadlessCredential, when set, is D2 step 4: a usable API key
// exists on disk or in env even if xai.api_key was not advertised.
HasHeadlessCredential func() bool
```

Grok spec:

```go
SafeAuthMethodIDs: []string{"cached_token", "xai.api_key"},
HasHeadlessCredential: grokHasAPIKey, // env or HasGrokConfigAPIKey; NOT auth.json
```

**Pure selector** `selectACPAuthMethod` in `authselect.go`:

```
func selectACPAuthMethod(
    advertised []string,   // Agent.Id values, order preserved
    pin string,            // Config.AuthMethodID
    safe []string,         // Spec.SafeAuthMethodIDs
    hasKey bool,           // Spec.HasHeadlessCredential()
) (methodID string, err error)
```

Rules, in order (MADR D2):

| step | condition | result |
| --- | --- | --- |
| 1 | `pin != ""` and `pin` ∈ advertised and `pin` ∈ safe | return pin |
| 1b | `pin != ""` otherwise | `err = fmt.Errorf("acp auth method %q: %w", pin, errUnsafeOrMissing)` |
| 2 | `cached_token` ∈ advertised (and ∈ safe, which it is for grok) | return `cached_token` |
| 3 | `xai.api_key` ∈ advertised | return `xai.api_key` |
| 4 | `hasKey` | return `xai.api_key` |
| 5 | else | return `""`, nil — caller applies D7 |

Membership tests are exact string match. Do not use
`defaultAuthMethodId` as a selector (MADR D2 last paragraph).

**`spawnAgent` after initialize decode** (replace `acpagent.go:456-472`):

1. Collect `advertised` from `initResp.AuthMethods` where
   `m.Agent != nil` (skip empty union values).
2. Optionally extend `grokInitializeMeta` with
   `DefaultAuthMethodID string \`json:"defaultAuthMethodId"\`` and log
   it at debug. Do not select on it.
3. `id, err := selectACPAuthMethod(advertised, p.cfg.AuthMethodID, p.spec.SafeAuthMethodIDs, hasKey)`.
4. If `err != nil`: kill process, return wrap (`spawnAgent` fails;
   prewarm logs "prewarm failed" and stores nothing).
5. If `id != ""`: `conn.Authenticate(initCtx, {MethodId: id})`. On
   error, kill, return `fmt.Errorf("acp authenticate (%s): %w", id, err)`.
   On success: `INFO acp authenticated method_id=…`.
6. If `id == ""` and `len(p.spec.SafeAuthMethodIDs) > 0`:
   set `s.authRequired = true`. **Do not warn.** Do not call
   `authenticate`.
7. If `id == ""` and `SafeAuthMethodIDs` is empty: keep today's warn
   iff `len(advertised)>0` (future-proof; grok will not hit this).

**`EnsureWarm`** after a successful `spawnAgent` (`:584`): if
`s.authRequired`, `s.markClosedAndKill()`, log
`INFO prewarm skipped: no headless-safe auth method`, return without
storing `p.warm`.

**`Start`** after claim/spawn, **before** `NewSession` (`:785`):

```
if s.authRequired {
    s.markClosedAndKill()
    return nil, fmt.Errorf("acp: authentication required (advertised %v): %w",
        advertisedSnapshot, ErrACPAuthRequired)
}
```

Store the advertised snapshot on the session at spawn so Start can
include it. `ErrACPAuthRequired` is a package sentinel so tests can
`errors.Is`. Message **must** contain `authentication required` so
existing `agenterr.KindAuth` classifiers fire.

Do **not** call `session/new` in this branch. That is the whole point
of D7 (G5).

**Unit tests** (`authselect_test.go`, plus spawn/start fakes if they
exist; otherwise selector + EnsureWarm/Start with a stub session):

| case | advertised | pin | hasKey | want |
| --- | --- | --- | --- | --- |
| LaunchAgent | cached_token, grok.com | "" | false | cached_token |
| cold | grok.com | "" | false | "" (D7) |
| cold + file key | grok.com | "" | true | xai.api_key |
| cold + advertised key | xai.api_key, grok.com | "" | false | xai.api_key |
| pin cached_token | cached_token, grok.com | cached_token | false | cached_token |
| pin grok.com | grok.com | grok.com | false | error |
| pin unknown | cached_token | envauth | false | error |
| pin unadvertised cached_token | grok.com | cached_token | false | error |
| three methods, prefer token | all three | "" | true | cached_token |

Plus: `TestEnsureWarmDropsAuthRequiredSpare`,
`TestStartFailsBeforeNewSessionWhenAuthRequired` (if Start can be
driven with a fake conn; otherwise test the guard function
extracted next to the selector).

Gate: pre-add-check; `go test ./internal/provider/acpagent/ ./internal/provider/grok/`. Commit.

---

### P4 — Catalog honesty for `grok.com` (MADR D3, D6)

**Files:** `internal/provider/grok/auth.go`, `auth_test.go`. Phone
fixtures only if a golden list breaks.

Add a third method on the single `xai` upstream:

```go
{
    ID:    xaiUpstreamID + ":browser", // "xai:browser"
    Type:  provider.AuthMethodOAuthBrowser,
    Label: "Sign in with Grok",
}
```

Do **not** set `Unavailable` / `Reason` on the provider. G12: the
transport annotator will emit `available:false` / `browser_only`.
Phone `isUsable` already excludes browser methods.

**Tests**

* `TestAuthStatusIncludesBrowserMethod` — three methods, types
  api / device / browser, ids `xai:api` `xai:device` `xai:browser`.
* Existing `internal/ws/method_availability_test.go` already covers
  browser annotation; add a grok-shaped fixture row only if that
  table is the canonical catalog test.

No Flutter code unless a snapshot lists exactly two grok methods.
If one does, update the snapshot and `dart format` that file.

Gate: pre-add-check; `go test ./internal/provider/grok/ ./internal/ws/`. Commit.

---

### P5 — live_grok pins (MADR D15 / confirmation 1–5, 7)

**Files:** `internal/provider/grok/live_auth_test.go` (new, `//go:build live_grok`),
reuse `live_helpers_test.go`.

These spend a real grok process. They must **not** call
`grok login`, **not** write `~/.grok/auth.json`, and **not** use the
host's real key. Isolate with `t.Setenv("HOME", tmp)` **and**
`t.Setenv("GROK_HOME", filepath.Join(tmp, ".grok"))`, and
`t.Setenv("XAI_API_KEY", "")` then `os.Unsetenv` as needed.

| test | steps | pass if |
| --- | --- | --- |
| `TestLiveInitializeAuthMethodsHost` | startACP initialize only against the real HOME (skip if no `auth.json`) | advertised set is `{cached_token, grok.com}` or the three-id set if `XAI_API_KEY` is set; `defaultAuthMethodId` is `cached_token` when the token method is present |
| `TestLiveColdInitializeOnlyGrokCom` | isolated HOME, initialize | advertised == `[grok.com]`; no defaultAuthMethodId |
| `TestLiveQuotedModelKeyAdvertisesAPIKey` | isolated HOME, write `[model."grok-4.5"]` **and** `[model."grok-4.6"]` in the fixture (whichever `currentModelId` grok picks on this install), initialize | `xai.api_key` ∈ advertised; `authenticate xai.api_key` → result (possibly `{}`); `session/new` returns `sessionId` |
| `TestLiveUnquotedModelKeyDoesNotCount` | isolated HOME, write unquoted `[model.grok-4.5]`, initialize + `authenticate xai.api_key` | `-32000` with `Set XAI_API_KEY or add api_key/env_key` |
| `TestLiveLegacyAuthTableDoesNotCount` | isolated HOME, write `[auth] api_key`, same RPCs | same `-32000` |
| `TestLiveColdSessionNewWithoutAuth` | isolated HOME, initialize, `session/new` **without** authenticate, 8s budget | either `-32000` or no response (document which this binary does); this pins G5 so D7's skip stays justified |
| `TestLiveUnknownMethod` | host or isolated, `authenticate does-not-exist` | `-32602` / `unsupported auth method` |
| `TestLiveLogoutAbsent` | `logout` | `-32601` |

`TestLiveQuotedModelKeyAdvertisesAPIKey` writes **both** current
static ids only as a **fixture convenience** so the test does not
have to guess `currentModelId` before initialize. Production write
path (P2) still writes one id. State that in the test comment so
nobody "fixes" P2 to match the fixture.

Also add a **non-live** test that `Provider.SetCredential` +
`resolveCredentialModel` against a fake catalog with
`DefaultIDs:[]string{"grok-4.6"}` writes only that table (P2 already
has this; P5 just must not regress it).

Run: `go test -tags live_grok ./internal/provider/grok/ -run Auth -count=1`.
Do **not** loop this. Commit the test file even if the live run is
left for acceptance (CI without grok will skip via `LookPath`).

Gate: `go test` (non-live) green; live run recorded in the phase
commit message body by the engineer who has grok. Commit.

---

### P6 — Comments and errata (MADR confirmation 8)

**Files (comments / one-line pointers only):**

* `internal/provider/acpagent/acpagent.go` — delete "correct for grok
  today" / "Agents that need no auth send an empty list". Replace
  with a pointer to MADR 0085 D2/D7.
* `internal/provider/acpagent/config.go:87-91` — `AuthMethodID` is an
  optional pin, rejected unless advertised and safe.
* `internal/config/config.go:476-479` — empty default is correct
  **because D2 auto-selects**; it is not "grok needs none".
* `internal/provider/credstore/write.go` function comments — describe
  quoted `[model."<id>"]`, not `[auth]`.
* `docs/0072-MADR-phone-reconnect-and-provider-timeout-incident.md` F9
  — add a one-line "superseded by 0085" note. Do not rewrite the
  incident narrative.
* `docs/0038-MADR-grok-acp-parity-assessment.md` §2.2 — one-line note
  that 1.0.3 authMethods are dynamic; see 0085.
* `docs/0083-MADR-provider-auth-activation-and-layout-gaps.md` A8 /
  matrix grok row — "config write works **only** as quoted
  `[model."<default>"]` (0085 D4)". Historical A1 "grok binds
  methodID to `_`" is already fixed for methodID; leave A1 unless
  still accurate after P2.

No behaviour change. Gate: pre-add-check on edited Go; markdown only
otherwise. Commit.

---

## Verification

Cross-phase acceptance (run after P5; maps to MADR Confirmation):

1. Isolated HOME, no credentials: `Start` returns `ErrACPAuthRequired`
   in well under 30s; log has no "session/new may fail"; no spare
   (`EnsureWarm` skip line).
2. Isolated HOME + `SetCredential` of a dummy key: `config.toml`
   contains exactly one `[model."<id>"]` matching
   `ListModels.DefaultIDs[0]` (or `Options[0]` if harvest was static);
   no `[auth] api_key`; next `Start` calls `authenticate xai.api_key`
   and `session/new` succeeds. (`XAI_API_KEY` unset.)
3. LaunchAgent-equivalent (real `auth.json`, no env key): initialize
   `cached_token`+`grok.com`; log `acp authenticated method_id=cached_token`;
   SuperGrok session still starts.
4. `ClearCredential` after (2): named table has no `api_key`;
   `auth.json` byte-identical; leftover `[auth]` gone if it existed.
5. After (2), no env, no `auth.json`: `AuthStatus.Status==configured`.
6. `providers.list` grok methods: `xai:api` usable, `xai:device`
   usable, `xai:browser` `available:false` `reason=browser_only`.
7. `go test -tags live_grok ./internal/provider/grok/ -run Auth -count=1`
   green on a machine with grok 1.0.3.
8. Comments in G10 locations no longer claim grok needs no auth.

**Commands**

```bash
make pre-add-check FILES="<phase files>"
go test -race ./internal/provider/credstore/ ./internal/provider/grok/ ./internal/provider/acpagent/ ./internal/ws/
go test -tags live_grok ./internal/provider/grok/ -run Auth -count=1
```

## Rollout and Rollback

**Rollout**

1. Land P1–P6 on `master` as six commits.
2. `make install` (or the repo's unsigned install path) so the
   LaunchAgent binary is `0.10.8.x` **after** P3. Restart
   `com.magiccliremote.mcremote`.
3. Confirm `~/Library/Logs/mcremote/mcremote.err.log` shows
   `acp authenticated method_id=cached_token` on this host, not the
   old warning.
4. Phone: grok provider sheet shows three methods; browser row
   disabled; paste-key still the default usable method.

**Rollback**

* P3 is the only behaviour change on a logged-in host. Revert that
  commit and reinstall: handshake returns to warn-and-skip;
  sessions still start (today's path).
* P1/P2: a host that already received a quoted-table write keeps
  working after rollback; grok reads that table without mcremote.
  A host that only had `[auth] api_key` is unchanged by rollback
  (that write never worked).
* P4 is additive catalog. Old phones ignore the extra method.
* No config migration, no protocol version bump, no capability flag.

**Risk**

* `ListModels` harvest during `SetCredential` on a never-prewarmed
  grok costs one initialize (~4s, `catalogProbeTimeout=30s`). Accept.
  Do not hold the WS handler longer: `SetCredential` already has the
  request context.
* Operator `providers.grok.auth_method_id: grok.com` (if anyone set
  it after reading 0072 F9) will start **failing** spawn (D2.1b).
  That is intended. Grep install docs / sample yaml: default is `""`.
* Default-model drift: a key written for `grok-4.5` does not apply
  after grok's default becomes `grok-4.6`. Documented in MADR
  consequences. Mitigation: paste again, or set `XAI_API_KEY`.
