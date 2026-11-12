<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

# Implement MADR 0074 — Remote provider auth from phone (W1 + W2 + W4 + W5)

> **Status, corrected 2026-10-18:** ~~in progress~~ complete 2026-11-12 (P24). The status bullet says both that P17–P22 "have not begun" and that they are "implemented as of 2026-08-21". The implementation log (§17.6) confirms the second. Only P22 steps 5–6, the live acceptance run that spends tokens, remain. Found by the record audit in [0200-REPORT-record-status-audit.md](../reports/0200-REPORT-record-status-audit.md). *Annotated 2026-11-12 (P23 step 9): P22 steps 5–6 moved to `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7, runs L1 and L2 (MADR D40), and P23 is complete; this note changes no other phase's status.* *Annotated 2026-11-12 (P24): P24 landed and this PLAN is complete. W3 (the loopback tunnel) is deferred to a record of its own; the MADR's 16.5 item 14 is owned by `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1.*

Associated MADR: [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md)

- **Status**: **implemented** — P1–P11 landed 2026-08-11/12 (commits `a5c408b`,
  `58906a6`, `e604146`, `03d435c`, `9429f5a`); **P12–P15 landed 2026-08-12** and
  are the full-vendor-coverage wave (MADR 0074 D16–D19). P16 records what is
  deliberately left. **Approved repair amendment 2026-08-21:** P17–P22
  implement accepted D20–D29 and have not begun. Verified
  against the tree, not against commit messages — see §0.2 and §17.
  **Approved amendment 2026-11-11:** P23 (§25) implements the MADR's §16
  (D30–D42), accepted the same day; ~~in progress~~ complete 2026-11-12
  (step 9), with the MADR's 16.5 item 14 open under
  `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1.
  **Cleanup step decided 2026-11-12:** P24 (§26), a cleanup after P23, ~~held on its
  Q24-A~~ approved 2026-11-12 with Q24-A answered (a), ~~not yet executed; when it lands this PLAN is `complete`~~
  executed 2026-11-12: this PLAN is **complete**, with W3 (the loopback tunnel) deferred to a record of its own
  and the MADR's 16.5 item 14 owned by
  `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1.
- **Date**: 2026-08-10, revised 2026-08-12; repair approved 2026-08-21; P23 approved 2026-11-11; P24 decided 2026-11-12
- **Scope**: Workstreams **W1** (auth status, credential injection, active-upstream
  switch), **W2** (device OAuth, Strategy A), **W4** (polish), and **W5** (full
  vendor coverage) from MADR 0074 §11.
  **W3 (loopback tunnel) is explicitly out of scope** and belongs to a successor
  MADR; no `oauth.loopback_tunnel_*` message may be added by this plan.
- **Standing rule (repo)**: every phase that writes or modifies code scopes its
  tests as explicit numbered Steps, never as passive Acceptance prose. Commit
  per phase; do not push until asked.
- **Hard gate**: phases P7–P10 (W2) do not start until the **W1 exit gate**
  (after P6) is green. P11 (W4) may run after P6. P12–P15 (W5) require P6.
  P17–P22 are implemented as of 2026-08-21; the token-spending acceptance run
  in P22 steps 5–6 is outstanding. P18/P19 were revised 2026-08-21 for MADR 0074
  F14 (server-side revocation) — see §17.5.

## 0. Grounding — code facts that bound this plan

| Fact | Where (verified 2026-08-10, tree `e4269ef`) |
| --- | --- |
| `ProviderInfoPayload` is exactly `{id, ready}` | `internal/protocol/messages.go:407` |
| `providers.list` / `providers.list_result` type constants | `internal/protocol/messages.go:88-89` |
| `Caps` v2 capability block; `Receipts bool` is the additive-flag precedent | `internal/protocol/messages.go:159` |
| Per-connection caps assembled here — where the new flag is set | `internal/ws/liveness.go:159` `capsFor` (spec renders at `:146`); called from `internal/ws/server.go:930` |
| WS request dispatch switch | `internal/ws/server.go:722` (`case protocol.TypeProvidersList`) |
| `handleProvidersList` — the function to extend | `internal/ws/server.go:1786` |
| Device-scoped handler precedent (own-data-only, MADR 0078 D8) | `internal/ws/server.go:1324` `handleReceiptsList` |
| `provider.Info` is `{ID, Ready}`; `List()` runs Ready probes outside the lock | `internal/provider/registry.go:49,56` |
| `Provider` interface (`ID`/`Ready`/`Start`) | `internal/provider/provider.go:265` |
| **Optional-interface pattern to copy** (`ModelCatalog`, implemented only by capable providers) | `internal/provider/provider.go:275` |
| Kilo dialect calls the engine via `o.h.API()(ctx, method, path, body, out)` | `internal/provider/kilo/session_ops.go:81` (`GET /mcp`) |
| Engine spawn inherits daemon env; no XDG isolation | `internal/provider/httpagent/provider.go:411-431` |
| Dart `ProviderInfo` model (`id`, `ready` only) | `apps/mobile/lib/data/protocol/models.dart:384` |
| Phone WS client | `apps/mobile/lib/data/ws/mcremote_client.dart` |
| Settings screen (ListTile pattern, 1272 lines) | `apps/mobile/lib/features/settings/settings_screen.dart` |
| No `provider.auth_*`, `set_credential`, or `oauth.*` symbol exists in Go today | verified by grep at `e4269ef` |

### 0.1 Decisions index (MADR 0074 §4.1)

D1 native per-agent write paths · D2 no parallel vault · D3 agent-native status ·
D4 additive `auth` block on `ProviderInfoPayload` · D5 typed method descriptors
with `inputs[]` · D6 `provider_auth` capability gate · D7 classify flows by URL,
not by `method` · D8 codex device auth is destructive → snapshot + confirm +
restore · D9 engine restart policy · D10 last writer wins · D11 phone write-only
secrets · D12 no Anthropic Pro/Max OAuth · D13 system browser, no in-app listener ·
D14 active-upstream switch · D15 live-tagged tests pin every auth surface ·
**D16** on-demand vendor catalog, separate from status · **D17** OpenCode
credentials go through its engine · **D18** goose catalog is pinned and its
writes are file-store-only · **D19** codex and grok stay single-vendor.

### 0.2 Host credential stores this plan writes (D1)

| agent | path / mechanism | write method |
| --- | --- | --- |
| kilo | daemon-owned engine | `PUT /auth/{providerID}` `{type:"api", key}` |
| opencode | daemon-owned engine (D17); `~/.local/share/opencode/auth.json` as fallback | `PUT /auth/{providerID}`; direct write, 0600, atomic when no engine |
| goose | `~/.config/goose/config.yaml` + `secrets.yaml` (keyring-disabled hosts only, D18) | direct write, 0600, atomic |
| codex | `~/.codex/auth.json` | `codex login --with-api-key` on stdin |
| grok | `~/.grok/config.toml` per-model `api_key` | direct write, 0600, atomic |

**Grok note.** D1 permits `XAI_API_KEY` in the service environment, but that
needs a service restart the daemon cannot perform on itself. This plan writes
`~/.grok/config.toml` instead; the env path stays an operator option.

### 0.3 Phase status, verified against the tree (2026-08-12)

Each row was checked by reading the code, not the commit that claims it. The
three 0074 commits that predate any implementation (`11902fe`, `8e2524d`,
`287b680`) are documentation-only and are the reason this table exists.

| phase | status | where it landed | notes |
| --- | --- | --- | --- |
| P1 protocol + capability | **done** | `internal/protocol/messages.go` (`TypeProviderAuthStatus` … `TypeOAuthCancel`, `Caps.ProviderAuth`) | shipped *beyond* the plan: `AuthInputOption` and `AuthInputCondition` (`when`) exist, which the plan did not call for and the live catalog needs |
| P2 interface + status probes | **done** | `provider/auth.go`, `provider/credstore/credstore.go`, per-agent `auth.go` | kilo fixture `testdata/provider-auth-7.4.20.json` committed as planned |
| P3 wire | **done** | `ws/liveness.go:168`, `ws/server.go` `handleProvidersList`, `pushProviderAuthStatus`, async dispatch | |
| P4 credential injection | **done for 4 of 5** | kilo (HTTP), opencode (file → engine in P13), codex (`login --with-api-key` on stdin), grok (`config.toml`) | **goose was not implemented as planned.** The plan called for a keyring write; that is not safely possible headlessly, and P14 replaces it with a decided, documented alternative (D18) |
| P5 active upstream | **done** | `goose/auth.go` `setActiveUpstream`, `kilo/upstream.go`, `opencode/upstream.go` | step 3's OpenCode half was **missing** until 2026-08-12 — the type assertion succeeded (httpagent declares the method) while the dialect hook did not exist, so the call returned `ErrAuthUnsupported`. Now implemented, mirroring kilo. `internal/provider/auth_conformance_test.go` is the guard that found it |
| P6 phone status + setup sheet | **done** | `models.dart`, `mcremote_client.dart`, `settings_screen.dart`, `provider_auth_sheet.dart` | step 3's `provider.auth_status` subscription was **missing** until 2026-08-12: the client dropped every server-pushed auth frame because the read loop only routed `event` and request replies. Now routed, exposed as `providerAuthStatus`, and the settings screen refreshes off it (D10's cross-device push had no effect before this) |
| P7 device-flow engine | **done** | `internal/providerauth/` (`classify.go`, `registry.go`, `cli.go`) | D7 classifier table-tested over the four captured responses |
| P8 kilo device OAuth | **done** | `kilo/device_auth.go` | |
| P9 grok + codex device flows | **done** | `grok/device_auth.go`, `codex/device_auth.go` | D8 sidecar lifecycle implemented |
| P10 phone device sheet | **half-done until 2026-08-12** | `device_flow_sheet.dart`, `settings_screen.dart` `_runDeviceSignIn` | the sheet and its widget tests existed but **nothing constructed it**, `startProviderDeviceAuth` had no caller, and the setup sheet answered "Device sign-in is not available yet" — so every device flow (Kilo Gateway, ChatGPT, Copilot) was unreachable from the phone despite the daemon side being complete. Now wired, including D8's destructive confirmation for codex |
| P11 polish (W4) | **done** | `agenterr.KindAuth` (`agenterr.go:38`), doctor auth section (`cli/doctor.go:61-93`), phone clear-credential | |
| **P12 catalog protocol** | **done** 2026-08-12 | `messages.go` (`TypeProviderAuthCatalog`, `AuthCatalogRequestPayload`, `AuthCatalogPayload`), `provider/auth.go` `AuthCataloger`, `ws/server.go` `handleAuthCatalog` | |
| **P13 OpenCode + Kilo full catalogs** | **done** 2026-08-12 | `httpagent/authcatalog.go`, `opencode/auth.go`, `kilo/auth.go` | 184 / 185 vendors live |
| **P14 goose catalog + file-store writes** | **done** 2026-08-12 | `goose/catalog.go` (73 vendors), `credstore.SetGooseSecret`, `ErrGooseKeyringManaged` | supersedes P4 step 4 |
| **P15 phone catalog browser** | **done** 2026-08-12 | `upstream_catalog_sheet.dart`, `settings_screen.dart` "Add credential" row | |
| **P16 deliberately not done** | — | — | see §16 |

---

## 1. Phase P1 — Protocol surface and capability gate (D4, D5, D6)

Types only. No behaviour change, no handler reads them yet. Landing the shapes
first keeps every later phase a pure addition.

**Steps**

1. In `internal/protocol/messages.go`, add type constants: `provider.auth_status`,
   `provider.set_credential`, `provider.clear_credential`,
   `provider.set_active_upstream`, `provider.start_auth`, `oauth.device_flow`,
   `oauth.device_flow_result`, `oauth.cancel`. Keep them adjacent to the existing
   `providers.*` block at `:88`.
2. Add `AuthInputPayload{Key, Type, Message, Options []string, Placeholder, Required}`
   — `Type` ∈ `text|select` (D5).
3. Add `AuthMethodPayload{ID, Type, Label, Inputs []AuthInputPayload}` —
   `Type` ∈ `api_key|oauth_device|oauth_browser`.
4. Add `UpstreamAuthPayload{ID, Label, Status, Methods []AuthMethodPayload}` —
   `Status` ∈ `configured|missing|error|quota`.
5. Add `ProviderAuthPayload{Status, ActiveUpstream string, Upstreams []UpstreamAuthPayload}`.
6. Extend `ProviderInfoPayload` (`:407`) with `Auth *ProviderAuthPayload \`json:"auth,omitempty"\``
   — pointer + `omitempty` so a daemon with nothing to say emits byte-identical v1 JSON (D4).
7. Add `SetCredentialPayload{ProviderID, UpstreamID, MethodID, Secret string, Inputs map[string]string}`
   and give it a `String()`/`LogValue()` that renders `Secret` as `[redacted]` (D2, D11).
8. Add `StartAuthPayload{ProviderID, UpstreamID, MethodID string, Inputs map[string]string, ConfirmDestructive bool}`,
   `DeviceFlowPayload{FlowID, VerificationURI, UserCode string, ExpiresIn, Interval int}`,
   `DeviceFlowResultPayload{FlowID, OK bool, Error, ErrorKind string}`, and
   `OAuthCancelPayload{FlowID string}`.
9. Add `ProviderAuth bool \`json:"provider_auth,omitempty"\`` to `Caps` (`:159`),
   mirroring `Receipts` exactly.
10. **Test** `internal/protocol`: a `ProviderInfoPayload{ID, Ready}` with nil `Auth`
    marshals to exactly `{"id":…,"ready":…}` — the v1 wire-compat guard for D4.
11. **Test** `internal/protocol`: `SetCredentialPayload` never renders the secret
    through `%v`, `%+v`, `slog`, or `json.Marshal` of its `LogValue()` (D2, D11).
12. **Test** `internal/protocol`: `Caps` with `ProviderAuth=false` omits the key.

**Exit**: `go build ./...` and `go test ./internal/protocol/` green; no other
package references the new symbols yet.

---

## 2. Phase P2 — Provider auth interface and read-only status probes (D3)

Add the optional interface and per-agent status readers. Still no writes and no
wire exposure.

**Steps**

1. In `internal/provider/provider.go`, next to `ModelCatalog` (`:275`), add:

   ```go
   // ProviderAuth is optionally implemented by providers that can report or
   // modify upstream credentials (MADR 0074 D3). Absent ⇒ the daemon reports
   // no auth block for that provider.
   type ProviderAuth interface {
       AuthStatus(ctx context.Context) (AuthState, error)
   }
   ```

2. Define `AuthState`, `UpstreamAuth`, `AuthMethod`, `AuthInput` in
   `internal/provider/` mirroring the P1 protocol shapes (domain types stay
   independent of wire types, as `picker.Catalog` does for models).
3. New package `internal/provider/credstore` with **read-only** helpers this
   phase: `ReadOpenCodeAuth(path)`, `ReadGooseConfig(path)`, `ReadGrokAuth(path)`,
   `CodexLoginStatus(ctx)`. Each returns provider ids/labels and presence —
   **never key material** (D2). Resolve paths through `internal/appdirs`.
4. Implement `AuthStatus` on the **kilo** provider using the engine API
   (`o.h.API()` pattern, `session_ops.go:81`): `GET /kilo/auth-status` and
   `GET /provider/auth`, mapping the live catalog (13 upstreams, 21 methods,
   8 with `prompts`) into `AuthState`. Map `prompts[]` → `AuthInput` (D5), and
   classify each method by MADR 0074 **D7** (URL-shape rule) into
   `oauth_device` vs `oauth_browser` vs `api_key`.
5. Implement `AuthStatus` for opencode, goose, grok, codex from step 3's readers.
6. Guard every probe with a short timeout (15s, matching `Diagnostics` in
   `kilo/session_ops.go:39`) and degrade to `status: "error"` rather than failing
   the whole listing.
7. **Test** `internal/provider/credstore`: golden-file parse of a representative
   `auth.json`, goose `config.yaml`, and `~/.grok/auth.json`; assert no value
   from any secret field appears in the returned struct (table-driven).
8. **Test** kilo `AuthStatus` against a recorded `GET /provider/auth` fixture
   captured from 7.4.20 (commit the fixture under `internal/provider/kilo/testdata/`):
   assert 13 upstreams, that the 8 prompt-bearing methods produce non-empty
   `Inputs`, and that the browser-mode OpenAI method classifies as
   `oauth_browser` while the headless one classifies as `oauth_device` (D7).
9. **Live test** (`//go:build live_kilo`) pinning the real engine's catalog shape,
   per D15.

**Exit**: `go test ./internal/provider/...` green; `go test -tags live_kilo
./internal/provider/kilo/` green on a host with kilo.

---

## 3. Phase P3 — Surface status on the wire (D3, D4, D6)

First user-visible change: the phone can *see* auth state. Read-only.

**Steps**

1. Extend `provider.Info` (`internal/provider/registry.go:49`) with
   `Auth *AuthState`, populated in `List()` only for providers implementing
   `ProviderAuth`. Keep the probes outside the registry lock, as the existing
   `Ready` probes already are (`:56`).
2. Set `caps.ProviderAuth = true` in `capsFor` (`internal/ws/liveness.go:159`),
   exactly where `caps.Receipts` is set (D6).
3. Extend `handleProvidersList` (`internal/ws/server.go:1786`) to fill
   `ProviderInfoPayload.Auth` **only when the connection negotiated the
   `provider_auth` capability**; otherwise leave it nil (D6).
4. Add `s.pushProviderAuthStatus(providerID)` emitting `provider.auth_status`
   to all capability-advertising connections; call it nowhere yet (P4 wires it).
5. Because `List()` now performs network I/O for kilo, route
   `TypeProvidersList` through `s.dispatchAsync` at `:722` — matching
   `TypeModelsList`, which boots engines for the same reason.
6. **Test** `internal/ws`: a v1 client's `providers.list_result` is byte-identical
   to today's (regression guard for D4/D6).
7. **Test** `internal/ws`: a v2 client without `provider_auth` gets no `auth` block.
8. **Test** `internal/ws`: a v2 client with the capability gets the block, and
   secret material appears nowhere in the frame.
9. **Test** `internal/ws`: a provider whose `AuthStatus` errors still yields a
   listing (degraded `status:"error"`), never a failed request.

**Exit**: phone (unchanged build) still works; a `websocat` v2 session shows the
auth block. Acceptance §4.3 item 4 satisfied.

---

## 4. Phase P4 — Credential injection (D1, D2, D9, D10)

The core of W1.

**Steps**

1. Extend the `ProviderAuth` interface with
   `SetCredential(ctx, upstreamID, methodID string, secret string, inputs map[string]string) error`
   and `ClearCredential(ctx, upstreamID string) error`.
2. **kilo**: implement via `PUT /auth/{providerID}` `{type:"api", key}` and
   `DELETE /auth/{providerID}` through the engine API. **No engine restart** (D9).
3. **opencode**: extend `credstore` with an atomic write (temp file in the same
   directory, `chmod 0600` before rename) that merges one `{type,key}` entry into
   `auth.json` without disturbing others. Then restart the shared engine (D9).
4. **goose**: atomic `config.yaml` merge plus keyring write via the existing
   platform keyring path; restart the shared engine (D9).
5. **codex**: spawn `codex login --with-api-key`, write the secret to stdin,
   close it, and wait with a timeout. Never pass the secret in argv (visible in
   `ps`). No restart (D9).
6. **grok**: atomic merge of a per-model `api_key` into `~/.grok/config.toml`,
   0600. No restart (D9).
7. Add a `credstore.Restart` hook the daemon calls per D9; refuse to restart
   while any session on that provider has a turn in flight and return a typed
   error the phone renders as "busy — try again after the current turn".
8. Wire `provider.set_credential` and `provider.clear_credential` handlers in
   `internal/ws/server.go` beside `handleReceiptsList` (`:1324`); dispatch at
   `:722` via `dispatchAsync`. On success call `pushProviderAuthStatus` (P3 step 4).
9. Enforce a max secret length (8 KiB) and reject empty secrets before any write.
10. Zero the secret slice after use in every write path.
11. **Test** each agent's write path against a temp HOME: assert file mode 0600,
    atomicity (no partial file on simulated failure), and that unrelated entries
    survive the merge.
12. **Test** codex path asserts the secret never appears in the child's argv.
13. **Test** the restart guard refuses mid-turn and succeeds when idle (D9).
14. **Test** concurrent `set_credential` from two simulated devices ends with one
    of the two values and one pushed status per device — last-writer-wins is
    explicitly asserted, not incidental (D10).
15. **Test** no secret reaches logs at any level: install a capturing `slog`
    handler and assert the secret substring is absent (D2).
16. **Live test** (`live_kilo`): real `PUT`/`DELETE` round-trip against a scratch
    provider id, restoring prior state afterwards (D15).

**Exit**: acceptance §4.3 items 1, 2, 7. Commit.

---

## 5. Phase P5 — Active upstream switch (D14) — the MADR 0073 fix

**Steps**

1. Add `SetActiveUpstream(ctx, upstreamID string) error` to `ProviderAuth`
   (optional-within-optional: return `provider.ErrUnsupported` where absent).
2. Implement for goose (`active_provider` in `config.yaml`, atomic write +
   engine restart per D9).
3. Implement for opencode and kilo by repointing the default model provider.
4. Wire `provider.set_active_upstream` in `internal/ws/server.go`; push status on success.
5. Reject a switch to an upstream whose status is `missing`, with a typed error.
6. **Test** goose switch rewrites only `active_provider` and preserves the other
   four configured providers on a fixture matching this host
   (`opencode_go`, `gemini_oauth`, `google`, `chatgpt_codex`, `xai_oauth`).
7. **Test** switching to an unconfigured upstream is refused.
8. **Test** `ErrUnsupported` surfaces cleanly for codex/grok.

**Exit**: acceptance §4.3 item 6. Commit.

---

## 6. Phase P6 — Phone: status chips and the setup sheet (D5, D11)

**Steps**

1. Extend Dart `ProviderInfo` (`apps/mobile/lib/data/protocol/models.dart:384`)
   with a nullable `auth` field and add `ProviderAuthInfo`, `UpstreamAuth`,
   `AuthMethod`, `AuthInput` models. Absent `auth` must keep today's behaviour.
2. Read `caps.provider_auth` in `mcremote_client.dart` and expose it; every auth
   affordance is hidden when false (D6).
3. Add client methods for `provider.set_credential`, `provider.clear_credential`,
   and `provider.set_active_upstream`, plus a subscription to `provider.auth_status`.
4. Providers section in `settings_screen.dart`: per-upstream chip rendering
   `configured | missing | error | quota`, following the existing ListTile idiom.
5. Build the setup sheet: render `inputs[]` generically — `text` → `TextField`,
   `select` → dropdown from `options` — then a masked key field for `api_key`
   methods. Inputs are optional unless `required` (the live catalog defaults
   them; MADR §7.3).
6. Mark `oauth_browser` methods disabled with "requires host access (see W3)".
7. Enforce D11 in code: the key controller is cleared and disposed on send and on
   sheet dismissal; no `SharedPreferences`/secure-storage write of the secret;
   `autocorrect: false`, `enableSuggestions: false`, `obscureText: true`.
8. Add the active-upstream switcher (D14).
9. **Test** (widget): a provider with no `auth` renders exactly today's tile.
10. **Test** (widget): a method with `select` + `text` inputs renders both and
    submits them in `inputs{}`.
11. **Test** (widget): the key controller holds no value after send.
12. **Test** (widget): `oauth_browser` renders disabled.

### W1 exit gate

`go test ./...` green; `flutter test` green; MADR 0074 §4.3 items **1, 2, 4, 6,
7, 8** demonstrated on a real host. **Do not start P7 until this is signed off.**

---

## 7. Phase P7 — Device flow engine (Strategy A, D7)

**Steps**

1. New `internal/providerauth/deviceflow.go`: a flow registry keyed by `flow_id`
   holding state, expiry, and a cancel func; single-use; swept on expiry.
2. Implement the **D7 classifier** as a standalone, unit-testable function:
   given an authorize URL and instruction string, return
   `device{user_code, verification_uri}` or `browser{}`. Rule: a `redirect_uri`
   query parameter pointing at `localhost`/`127.0.0.1` ⇒ browser; else extract a
   code from the URL query or from `Enter code: <CODE>`.
3. Add `StartDeviceAuth(ctx, upstreamID, methodID string, inputs map[string]string) (DeviceFlow, error)`
   to the `ProviderAuth` interface (optional; `ErrUnsupported` by default).
4. Wire `provider.start_auth` and `oauth.cancel` handlers; emit `oauth.device_flow`
   then `oauth.device_flow_result`. Bind each flow to the initiating device id,
   exactly as `handleReceiptsList` scopes by device (`:1324`).
5. Cap concurrent flows per device (2) and overall (8); expire at the provider's
   `expires_in` or 15 minutes, whichever is shorter.
6. **Test** the classifier as a table over the four **real captured responses**
   in MADR 0074 §7 — kilo Gateway, OpenAI headless, GitHub Copilot, OpenAI
   browser — asserting the first three are `device` and the last is `browser`.
   This is the regression guard for the inverted assumption the MADR corrected.
7. **Test** flow expiry, cancellation, single-use, and per-device binding.
8. **Test** a second device cannot cancel or observe another device's flow.

**Exit**: `go test ./internal/providerauth/` green. Commit.

---

## 8. Phase P8 — Kilo engine-hosted device OAuth

The proven surface; do it first.

**Steps**

1. Implement `StartDeviceAuth` on kilo: `POST /provider/{id}/oauth/authorize {method, inputs?}`,
   classify via P7, then poll `GET /kilo/auth-status` (or `GET /provider/auth`)
   until authenticated, the flow expires, or it is cancelled.
2. Poll at the provider-supplied `interval`, defaulting to 5s, with jitter and a
   hard ceiling on request count.
3. On success push `provider.auth_status`; on failure emit
   `oauth.device_flow_result` with an `agenterr`-classified `error_kind`.
4. Do **not** call `oauth/callback` for `auto`-mode flows — MADR 0074 §7.1 shows
   the engine completes them itself.
5. **Test** against a stubbed engine: success, expiry, cancel, and engine-down.
6. **Live test** (`live_kilo`, opt-in and skipped by default because it needs a
   human to enter a code): drive a real Kilo Gateway device authorization and
   assert `auth-status` flips. Document the manual step in the test's doc comment.

**Exit**: acceptance §4.3 item 3. Commit.

---

## 9. Phase P9 — Grok and Codex CLI device flows (D8)

**Steps**

1. Add a CLI device-flow driver that spawns a login command, streams stdout,
   **strips ANSI escapes**, and extracts the verification URI and user code.
2. **grok** (1.0.0): `grok login --device-auth`.
3. **codex** (0.146.0): `codex login --device-auth`. Parse the pinned format from
   MADR 0074 §5.3 — static URL `https://auth.openai.com/codex/device`, code
   `[A-Z0-9]{4}-[A-Z0-9]{5}`, 15-minute expiry.
4. **Implement D8 for codex, in this order**: (a) copy `~/.codex/auth.json` to a
   0600 sidecar; (b) refuse to start unless `StartAuthPayload.ConfirmDestructive`
   is true, returning a typed `confirmation_required` error naming the
   consequence; (c) on failure, cancellation, timeout, or daemon shutdown mid-flow,
   restore the sidecar and verify it byte-matches; (d) delete the sidecar only
   after a confirmed success.
5. Kill the process group on cancel — the vendored codex binary is a *grandchild*
   of the npm shim and survives a plain kill (observed 2026-08-10). Reuse
   `procutil.TerminateProcessGroup`.
6. **Test** the parser on the captured codex output fixture including ANSI bytes,
   and on a truncated/garbled variant that must fail cleanly rather than emit a
   wrong code.
7. **Test** the D8 lifecycle with a fake codex: sidecar created, restore-on-cancel
   byte-identical, restore-on-timeout, sidecar removed on success, and start
   refused without confirmation.
8. **Test** cancel reaps the whole process group (no orphan).
9. **Live test** (`live_codex`, skipped by default, destructive): documented
   manual procedure mirroring the MADR's probe, including its restore step.

**Exit**: acceptance §4.3 item 5. Commit.

---

## 10. Phase P10 — Phone: device flow sheet (D13)

**Steps**

1. Device sheet: large monospaced user code with copy-to-clipboard, verification
   URL button opening the **system browser** via `url_launcher` (D13 — no in-app
   listener, no `ASWebAuthenticationSession`), expiry countdown, live status, cancel.
2. Destructive confirmation dialog for codex naming the consequence verbatim:
   "This signs the host out of ChatGPT immediately, before you finish signing
   in." Only on confirm is `confirm_destructive: true` sent (D8).
3. Handle `oauth.device_flow_result` — success closes the sheet and refreshes
   status; failure shows the `error_kind` copy.
4. Cancel on sheet dismissal and on app backgrounding beyond the resume window.
5. **Test** (widget): code renders and copies; countdown ticks; cancel emits
   `oauth.cancel`.
6. **Test** (widget): codex flow cannot start without passing the confirmation.

**Exit**: W2 complete. Commit.

---

## 11. Phase P11 — Polish (W4)

**Steps**

1. Extend `internal/agenterr` to classify 401/invalid-key/revoked-token
   alongside the existing quota and rate-limit kinds, so a dead credential
   presents as `status: "error"` rather than a generic turn failure.
2. Map that classification into the upstream chip so an expired key is visibly
   distinct from a missing one and from a quota block.
3. Add auth status to `mcremote doctor`, with key values redacted (MADR 0074 §9).
4. Phone-side revoke/logout per upstream, calling `provider.clear_credential`.
5. **Test** the classifier over captured 401 bodies from at least two agents.
6. **Test** doctor output contains provider ids and never key material.

**Exit**: commit.

---

## 11a. Phase P12 — Catalog protocol and interface (D16)

Types and one handler. The split from status is the whole point: status is
small and rides on every listing, the catalog is ~185 vendors and is fetched
when the user goes looking.

**Steps**

1. In `internal/protocol/messages.go`, add `provider.auth_catalog` and
   `provider.auth_catalog_result` beside the other `provider.*` constants.
2. Add `AuthCatalogRequestPayload{ProviderID, Query, Offset, Limit}` and
   `AuthCatalogPayload{ProviderID, Upstreams, Offset, Total, Truncated, Source}`,
   plus the `engine`/`static` source constants.
3. In `internal/provider/auth.go`, add `AuthCataloger` — optional, like
   `ModelCatalog` — returning `AuthCatalog{Upstreams, Source}`.
4. Wire `handleAuthCatalog` in `internal/ws/server.go` beside the other auth
   handlers, gated on the `provider_auth` capability (D6) and dispatched async
   (it may boot an engine).
5. Filter server-side on `Query` (case-insensitive, id or label) and page with
   a 100 default / 200 cap; set `Truncated` when more follows.
6. Factor the status block's upstream→wire conversion into one helper both
   paths use, so the two can never describe a method differently.
7. **Test** `internal/ws`: an unconfigured vendor appears in the catalog and
   not in status.
8. **Test** `internal/ws`: query narrows by id and by label, case-insensitively.
9. **Test** `internal/ws`: paging is contiguous, the last page is not flagged
   truncated, and an oversized `limit` is clamped rather than honoured.
10. **Test** `internal/ws`: a client without the capability is refused; an
    agent with no catalog answers `unsupported` rather than failing.

**Exit**: `go test ./internal/ws/ ./internal/protocol/` green.

---

## 11b. Phase P13 — OpenCode and Kilo full catalogs (D16, D17)

**Steps**

1. New `internal/provider/httpagent/authcatalog.go`, shared because kilo is an
   OpenCode fork and both engines answer the same endpoints:
   `FetchVendorCatalog` (`GET /provider` → vendors + connected set),
   `FetchAuthMethods` (`GET /provider/auth` → typed methods),
   `BuildCatalog` (merge; a vendor with no typed method gets an API-key one),
   and `ClassifyCatalogMethod` moved out of the kilo dialect.
2. Add `AuthCatalogDialect` and `Provider.AuthCatalogList` to httpagent;
   the catalog always needs an engine, so it calls `ensureServer`.
3. Implement `AuthCatalogList` on kilo and opencode over `EngineCatalog`.
4. Rewrite opencode's `AuthStatus` to prefer the engine — the connected set
   also covers env-keyed vendors, which `auth.json` never shows — and keep the
   file+env read as the cold-host fallback. Read it from
   `GET /config/providers`, **not** `GET /provider`: the second is 4.7 MB and
   this runs on every `providers.list`.
5. Implement `SetCredential`/`ClearCredential` on the opencode dialect via
   `PUT`/`DELETE /auth/{id}` (D17). Keep `SetCredentialFile`/`ClearCredentialFile`.
6. In `httpagent`, fall back to the file writer when the engine cannot be
   started, instead of failing the write: a cold host is exactly when the phone
   most needs to add a credential.
7. Cache the fetched catalog per engine (5 minutes) and invalidate it on every
   credential write or clear — the phone pages and searches against it, and it
   carries the per-vendor status a write just changed.
8. **Test** opencode against committed fixtures captured from 1.18.16: the
   catalog carries `togetherai`, `deepseek`, `anthropic`, `groq` with API-key
   methods and real display names.
9. **Test** typed methods survive the merge (copilot keeps `deploymentType`
   and `enterpriseUrl`), and OpenAI's browser and headless flows classify
   differently (D7's catalog-time hint).
10. **Test** status stays small, never carries an unconfigured key-only vendor,
    and never fetches the vendor catalog (the fixture server fails on any
    unexpected path, which is the assertion).
11. **Test** the write path calls `PUT /auth/{id}` with the key in the body,
    never in the path, and rejects an empty secret before any call.
12. **Test** kilo's catalog covers the same key-only vendors against a
    committed 7.4.21 `GET /provider` fixture.
13. **Test** the catalog cache serves a second read, expires, and is dropped by
    a credential write.
14. **Live test** (`live_opencode`, `live_kilo`): assert the real counts, that
    one page fits a phone frame, and that status has not collapsed into the
    catalog. A write round-trip is opt-in behind `MCREMOTE_LIVE_AUTH_WRITE=1`
    because it touches the host's real store.

**Exit**: MADR 0074 §4.3 items 9 and 11.

---

## 11c. Phase P14 — Goose catalog and file-store writes (D18)

This phase supersedes P4 step 4, which called for a keyring write. See §16.

**Steps**

1. Transcribe goose's provider registry into `internal/provider/goose/catalog.go`
   — id, display name, and the secret name each vendor's key is read under —
   from the vendor's own metadata: `declarative/definitions/*.json`,
   `ProviderMetadata`+`ConfigKey`, and `canonical/catalog.rs`. Record the goose
   version the table is pinned to.
2. Give vendors with no key (subscription and CLI-backed: `chatgpt_codex`,
   `gemini_oauth`, `xai_oauth`, `github_copilot`, …) a host-side sign-in method
   rather than a key field that cannot work.
3. Implement `authCatalogList` returning that table with `Source: static`.
4. In `credstore`, add `GooseSecretsPath`, `GooseKeyringDisabled` (env **and**
   `config.yaml`), `ReadGooseSecretNames`, `SetGooseSecret`, `DeleteGooseSecret`
   — atomic, 0600, merging rather than replacing the document.
5. Implement goose `setCredential`/`clearCredential`: refuse a keyless vendor,
   refuse with `ErrGooseKeyringManaged` on a keyring-backed host, otherwise
   merge into `secrets.yaml`.
6. Widen `configuredUpstreams` to include vendors whose secret mcremote wrote,
   so a credential set from the phone reads back as configured and
   `setActiveUpstream` will accept it.
7. **Test** the catalog covers ≥60 vendors including `together`, `xai`,
   `anthropic`, `opencode_go`, with the labels goose itself uses.
8. **Test** a write lands under the vendor's own secret name at 0600 and reads
   back as configured.
9. **Test** a second vendor's key does not erase the first, and a clear removes
   only its own.
10. **Test** a keyring-backed host is refused **and no file is written**.
11. **Test** a keyless vendor is refused.
12. **Test** the active-upstream switch accepts a phone-configured vendor and
    still refuses an unknown one.

**Exit**: MADR 0074 §4.3 items 10 and 12.

---

## 11d. Phase P15 — Phone: catalog browser (D16)

**Steps**

1. Add `ProviderAuthCatalog` to `models.dart` — upstreams, offset, total,
   truncated, source.
2. Add `listUpstreamCatalog` to `mcremote_client.dart`, returning null (not an
   error) for `unsupported`/`unknown_provider`: codex and grok have exactly one
   vendor each, which is a shape of the world, not a failure.
3. New `upstream_catalog_sheet.dart`: search field with a 250 ms debounce, a
   generation counter so a slow response for an old query cannot overwrite a
   new one, infinite scroll that pages, and a subtitle that says
   "showing N of M" and flags a pinned list.
4. Render vendors whose only method is browser OAuth as disabled with
   "Requires host access" — they need W3.
5. Add an "Add credential · <agent>" row per agent in the providers section
   that opens the sheet and, on a pick, drops into the existing setup sheet.
6. **Test** (widget): a vendor with no credential is listed.
7. **Test** (widget): search reaches the daemon with the query and narrows.
8. **Test** (widget): a partial list says how much it is showing; a pinned
   list says it is pinned.
9. **Test** (widget): browser-only vendors are not tappable.
10. **Test** (widget): an agent with no catalog renders an empty state.
11. **Test** (widget): picking a vendor returns it to the caller.

**Exit**: `flutter test` green.

---

## 12. Verification

**Per-phase**: `go build ./...`, `go test ./...`, and `flutter test` in
`apps/mobile` for phases touching Dart. `golangci-lint` clean.

**Acceptance (MADR 0074 §4.3)** — W1 gate covers 1, 2, 4, 6, 7, 8; W2 adds 3 and 5:

1. Cold host, phone pastes an OpenCode Go key, prompt completes — no SSH. (P4/P6)
2. Kilo key via `PUT /auth`, turn runs with **no** engine restart. (P4)
3. Kilo Gateway device authorization completes end-to-end. (P8)
4. Phone without `provider_auth` sees today's UI exactly. (P3)
5. Codex device auth shows the destructive confirmation; cancelling leaves
   `~/.codex/auth.json` byte-identical. (P9)
6. Goose switched off a quota-exhausted upstream from the phone. (P5)
7. No secret in daemon logs at info level after every flow above. (P1/P4)
8. Live-tagged tests fail when a pinned CLI's auth output changes. (P2/P4/P8/P9)

9. Cold host, phone finds Together AI in OpenCode's catalog by search, pastes
   a key, and the vendor reads back configured with no restart. (P13/P15)
10. The same search finds Together AI under Kilo and under goose, and goose's
    answer is labelled pinned. (P13/P14/P15)
11. A catalog page stays inside a phone-sized frame. (P12/P13)
12. A goose write on a keyring-backed host fails with guidance and writes
    nothing. (P14)

**Standing safety rule for anyone running these probes**: snapshot every
credential store to a 0600 sidecar first. The MADR's own research deleted a live
codex credential; it was restored only because a backup existed. The 2026-08-12
OpenCode write probe avoided the question entirely by booting the engine with an
isolated `XDG_DATA_HOME`, which is the better pattern for anyone repeating it.

## 13. Rollout and Rollback

**Rollout.** Every phase is additive and dark by default. The feature becomes
reachable only when a phone build advertises `provider_auth`; a daemon ahead of
its phone changes nothing observable (P3 tests pin this). Ship W1 to one host,
confirm the §4.3 items, then widen. W2 ships behind the same capability and adds
no new config surface.

**Rollback.** Phase-granular `git revert` — no schema migration, no persisted
mcremote state, and no change to any agent's credential format. Clearing
`caps.ProviderAuth` in `capsFor` disables the entire feature at runtime without a
revert, since every phone affordance is gated on it (D6). Credentials already
written by the feature are the agents' own files and survive a rollback intact;
they can be removed with each agent's native logout.

**Residual risk.** D1 couples mcremote to three third-party on-disk formats
(`opencode auth.json`, goose `config.yaml`, `~/.grok/config.toml`). A vendor
format change breaks writes; D15's live tests convert that into a CI failure
rather than a field incident. Kilo, which uses a supported HTTP API instead of a
file, carries none of this risk — the argument for leading with it.

---

## 16. P16 — Deliberately not done

Recorded so the next reader can tell a decision from an oversight.

| item | why not | what would change it |
| --- | --- | --- |
| **Goose OS-keyring writes** (the original P4 step 4) | Every portable way to write a keychain from a daemon either passes the secret in `argv`, where any process can read it from `ps` — which D2 forbids and P4 step 12 explicitly tests against — or needs an interactive unlock no headless flow can answer. Goose reads one store or the other, so writing `secrets.yaml` on a keyring host would look like success and change nothing. | A vetted in-process keychain binding (cgo Security.framework on macOS, libsecret on Linux) that never puts the secret on a command line, plus the MADR 0069 work on what a launchd daemon may actually reach. |
| **Codex third-party model providers** | Codex reads a non-OpenAI vendor's key from an environment variable named by `model_providers.<id>.env_key`. Injecting that at spawn means mcremote holds the secret itself — the parallel vault D2 exists to prevent. `experimental_bearer_token` would allow an in-config key, but its name says how stable it is. | Codex giving the inline token a supported name, or an auth-store entry per provider. |
| **Copilot device auth through OpenCode/Goose** (W2 tail) | The Kilo, Grok and Codex device flows cover the demand; Copilot through the other two agents adds a third code path for the same vendor. | Demand, or Copilot becoming the primary upstream on a host. |
| **W3 loopback tunnel** | Its own MADR, by decision (MADR 0074 §10). GitLab, Snowflake, DigitalOcean and OpenAI's browser flow stay SSH-bound and are shown disabled rather than hidden. | The successor MADR. |
| **Live drift guard for goose's table** | Goose exposes no listing to compare against, so there is nothing to assert in CI. A vendor configured on the host is always shown, table or no table, which bounds the damage. | Goose growing a `goose providers --json`-shaped command. |

---

## 17. Approved repair amendment — transactional Codex and Grok credentials

This section implements accepted MADR 0074 D20–D29. It repairs the existing
P9/P10 composition; it does not reopen P1–P16 or add another credential source.

### 17.1 Scope and invariants

**In scope**

* Codex and Grok device-login isolation, validation, conditional publication,
  generation retention, startup recovery, and autonomous-refresh reconciliation.
* One shared provider mutation coordinator used by device login, API-key writes,
  explicit logout, watcher reconciliation, and recovery.
* Ownership of every spawned login process from reservation through terminal
  cleanup, including response-write failure and daemon shutdown.
* Truthful phone cancellation and recovery/activation states.
* Unit, helper-process, race, Flutter, and version-pinned live coverage.

**Out of scope**

* A database, mcremote credential source, general secret vault, OS-keystore
  dependency, or encrypted-at-rest generation format. Those require a separate
  accepted decision and must not be smuggled into this repair. P17–P22 use D23's
  private `0700` directories and `0600` immutable payloads.
* Changes to Codex or Grok's native live credential format.
* W3 browser-loopback tunnelling, provider-specific OAuth RPC replacement, or
  auth work for Kilo, OpenCode, or Goose.
* Automatic restoration over an unknown external deletion, explicit logout, or
  a live file whose fingerprint is not proven by the transaction journal.

The release invariant is:

```text
incomplete flow  => LIVE byte-identical
successful flow  => validated PENDING atomically becomes LIVE and CURRENT
second success   => old CURRENT becomes PREVIOUS; exactly two committed copies
explicit logout  => tombstone committed, LIVE absent, retained payloads absent
ambiguous crash  => preserve all evidence, report recovery_required, overwrite nothing
```

### 17.2 Phase boundaries and commit cadence

P17–P22 are separately reviewable commits. At the end of every phase:

1. Run the phase's focused tests.
2. Run `make pre-add-check FILES="<changed Go files>"` before staging any Go
   file; run Dart formatting before staging Dart files.
3. Stage only that phase, review `git diff --cached`, and run
   `git commit --no-edit`.
4. Do not push unless the owner asks in that same turn.

Do not commit a later phase to make an earlier phase's tests pass. If execution
reveals a new architectural choice, amend the MADR and this plan and wait for
approval.

| MADR decision | Implemented and confirmed in |
| --- | --- |
| D20 keep both device flows online | P18 (dark construction), P20 (atomic activation), P21, P22 |
| D21 shared transaction coordinator | P17, P18 |
| D22 effective homes and isolated pending login | P18, P22 |
| D23 CURRENT/PREVIOUS generations | P17, P19, P22 |
| D24 backup lifecycle | P17, P19, P22 |
| D25 conditional validation/publication | P17, P18, P22 |
| D26 crash recovery | P17, P19, P22 |
| D27 owned flow lifecycle | P18 (handle contract), P20 (server ownership), P22 |
| D28 truthful phone cancellation/state | P20 (wire capability/state), P21 |
| D29 boundary and fault tests | every phase, final P22 gate |

### 17.3 Current-tree facts that determine the repair

These locations were re-read on 2026-08-21. They are implementation inputs,
not assumptions:

| current fact | evidence | required plan consequence |
| --- | --- | --- |
| Codex reads LIVE into memory and then starts the destructive CLI against the effective live home | `internal/provider/codex/device_auth.go:56,79`; the provider comment at `:24` records the upstream deletion | P18 replaces backup/restore with a pending `CODEX_HOME`; no child may see LIVE while authenticating |
| Grok starts `grok login --device-auth` directly and has no credential transaction | `internal/provider/grok/device_auth.go:42,86` | P18 gives Grok the same isolation, validation, and publication path as Codex |
| The WebSocket handler starts provider side effects before registry admission and detaches from request cancellation with no server-owned handle | `internal/ws/server.go:2307-2313` | P20 reserves first, attaches an owned handle, and derives lifetime from `Server.lifeCtx` |
| Registry `Add` combines admission with already-started flow registration | `internal/providerauth/registry.go:60,77` | P20 splits `Reserve` from `Attach` and holds capacity through terminal cleanup |
| CLI flow has independent `Wait` and `Kill` entry points with no shared terminal ownership contract | `internal/providerauth/cli.go:175,189` | P18 makes them idempotent views of one terminal result; P20 installs one owner |
| Atomic writes ignore directory-sync failure | `internal/fsutil/atomic.go:84` | P17 makes directory durability failure observable and fault-injectable |
| Codex/Grok credential paths use HOME-only helpers | `internal/provider/credstore/credstore.go:145,155,164` | P18 resolves `CODEX_HOME`/`GROK_HOME` once and reuses that result everywhere |
| Credential clear has no method identity | `internal/provider/auth.go:191`; `internal/ws/server.go:2226,2238` | P18/P20 add optional method-specific clear without breaking other providers or older clients |
| Provider instances are created before the session manager and its `LiveCountFor` callback | `internal/daemon/daemon.go:159,220,308,312` | P20 reorders construction so coordinated providers receive the real quiescence callback before prewarm |
| `fsnotify` is already present transitively and builds are `CGO_ENABLED=0` | `go.mod`; repository build targets | P19 may promote `fsnotify` to direct use but adds no database, cgo, or keystore dependency |

The installed CLI contracts were also checked read-only on 2026-08-21 against
codex-cli 0.148.0 and Grok 1.0.5: `codex login --device-auth`,
`codex login --with-api-key`, `codex logout`, `grok login --device-auth`, and
`grok logout` are all present. P18 still pins their behavioral assumptions with
helper and live-tagged tests; command availability alone is not treated as proof
of safe filesystem behavior.

### 17.4 Fixed operational bounds

Implement these as package constants with tests, not new user configuration in
this repair:

| bound | value | application |
| --- | --- | --- |
| credential payload | 1 MiB | reject larger LIVE, candidate, or generation files before allocation/copy |
| coordinator/native lock acquisition | 5 seconds | return `ErrTransactionBusy`; never wait forever behind a crashed or external writer |
| provider validation/logout probe | 30 seconds | kill and reap the isolated process group on expiry |
| watcher debounce | 250 milliseconds | coalesce create/write/rename bursts on the parent directory |
| stable-read interval/deadline | 100 milliseconds / 2 seconds | require two identical validated fingerprints or classify the observation unstable |
| post-auth activation grace | 5 minutes, capped by the original flow deadline | retain owned validated PENDING while waiting for provider idle |
| shutdown flow/watcher drain | 10 seconds per stage | report retained ownership on timeout and preserve disk evidence |

Changing these values after implementation is an operational tuning change;
changing retention count, publication conditions, or recovery outcomes requires
a MADR amendment.

### 17.5 Revision — 2026-08-21: server-side revocation (F14)

A pre-execution audit re-verified this repair against the working tree at
`07a17a6` and the installed `codex-cli 0.148.0`. Every F1-F13 finding, file,
line, and configuration-key citation held, including `cli_auth_credentials_store`
and its File/Keyring/Auto/Ephemeral variants, and the discarded directory-sync
error at `internal/fsutil/atomic.go:83-85` that P17 step 1 exists to fix. Each
of P17-P22 already carried affected files, numbered steps, verification
commands, and acceptance criteria, so the phases were executable as written.

The audit established MADR 0074 **F14**: Codex's pre-login clear calls
`logout_with_revoke`, which POSTs the stored refresh token to `/oauth/revoke`
before deleting the file. Revocation is skipped when no auth is stored and
applies only to `auth_mode == Chatgpt`; API-key credentials are never revoked;
revoke failure is a warning and deletion proceeds anyway. Four plan changes
follow:

| # | Change | Why |
| --- | --- | --- |
| 1 | **P18 step 5** now states that seeding CURRENT means writing the generation store under `<DataDir>`, not the pending home, and requires a test asserting the pending home is empty of credential material at spawn. | "Seed CURRENT before spawn," sitting next to "create a private pending home," invited seeding the *home*. That would have made the child revoke the live refresh token server-side while LIVE stayed byte-identical and every fingerprint check passed — a silent sign-out that the filesystem-level acceptance criteria could not have caught. |
| 2 | **P18 step 11** splits native logout by whether the invocation revokes. Codex ChatGPT logout writes its durable tombstone before invoking, against the effective live home; API-key and Grok keep the clone-and-verify probe. | The original clone-and-probe was unsound for ChatGPT credentials: the clone carries the same refresh token, so the "probe" revoked the grant before any fingerprint comparison could decide to roll back. Its stated guarantee — "a failed probe or changed LIVE fingerprint changes neither LIVE nor the manifest" — was unachievable. Zero exit also does not prove revocation, since upstream only warns on failure. |
| 3 | **P18 acceptance** adds pending-home emptiness and tombstone-before-revoke criteria. | The prior criteria proved LIVE was byte-identical, which F14 shows is insufficient: bytes can survive intact while the grant behind them is dead. |
| 4 | **P19 step 5** adds a `reauth_required` state and forbids `recovery_available: true` when every candidate is known-revoked. | Recovery could otherwise offer the operator a restore guaranteed to fail. |

Scope, phase order, file lists, and verification commands are otherwise
unchanged, and no work outside D20-D29 was added.

### 17.6 Implementation log

| Phase | Commit | Verification | Evidence | Notes |
| --- | --- | --- | --- | --- |
| P17 | `518f1df` | `go test -count=1 ./internal/fsutil ./internal/providerauth` and `go test -race -count=1 ./...` (same two packages) green; `make pre-add-check` reports 11 files clean; `go vet` clean; `./internal/ws ./internal/provider/codex ./internal/provider/grok ./internal/daemon` still green | 50 focused tests across the transaction core, the full P17 step 9 recovery table, and six helper-process kill points | Adds `adapter.go`, `manifest.go`, `store.go`, `transaction.go`, `recovery.go` and their tests; strengthens `fsutil.WriteFileAtomic`. No provider or WebSocket behavior changed, so nothing is reachable from production construction yet. |
| P18 | **Complete** — `ff3bb65`, `c02b66a`, `3508b4c`, `8778dac`, `88a4bb1`, `2eb8f0b`, `076b3f4` | `go test -count=1 ./internal/...` fully green; `-race` green on codex, grok, credstore, providerauth, ws; `make pre-add-check` clean on every changed file | All 14 steps; both adapters, the shared owned-flow engine, both coordinated providers, the logout split, and the API-key transaction | Still dark: no production daemon call site constructs a coordinated provider, so the pre-P20 device-auth path is what executes. P20 activates it. *Annotated 2026-11-11 (MADR §16 D38, D41): steps 9 and 11 were built but not wired to the phone's codex key write and logout, which stayed direct until P23 step 7; step 7's grok cached-token probe was never built and is not added (D41).* |
| P19 | **Complete** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Watcher, reconciliation, operator CLI, and `RecoverAll`; `fsnotify` promoted to a direct dependency (it is the inotify wrapper on Linux, and a watcher that cannot start degrades to reconciliation). | |
| P20 | **Complete** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Reservation-based registry, owned server flow, disconnect/resume, shutdown drain, additive wire contract, daemon activation, CLI registration. **This is the phase that made the fix live.** | |
| P21 | **Complete** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Idempotent cancellation behind every dismissal path, transactional copy, `ready_to_activate` rendering, per-method removal. | |
| P22 | **Complete except the acceptance run** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Live-tagged isolation tests for both providers, convergence-under-kills test, operator recovery guide, full matrix. | `make live-codex` and `make live-grok` both green 2026-08-21; a `live-grok` target was added, since the phase referenced one that never existed. Real credentials verified byte-identical before and after. Three pre-existing `live_grok` turn-behaviour failures reproduce at `9276ada` and are unrelated. The completed-login, rotation, busy-activation, and logout run is left for the owner: it spends tokens and revokes a real grant. *Annotated 2026-11-12 (MADR §16 D40, P23 step 9): that run, P22 steps 5 and 6, moved to `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7, runs L1 and L2, and is not run under this phase.* |

#### P18 progress note (as of `10093d3`)

Complete:

* **Step 1** — effective-home helpers. `credstore.CodexHome`/`GrokHome` prefer
  `CODEX_HOME`/`GROK_HOME`, and every auth path, lock path, and child
  environment derives from that one result, closing F7/F10.
* **Step 2** — `codex.DetectCredentialStore` reads the top-level
  `cli_auth_credentials_store` key and returns a wrapped
  `ErrUnsupportedBackend` for keyring, auto, ephemeral, and unrecognized
  values. A same-named key under a `[table]` header is correctly ignored.
* **Step 3** — `provider.OwnedDeviceAuth`, `provider.DeviceAuthHandle`,
  `provider.DeviceAuthUpdateSource`, and `provider.AuthMethodClearer` are
  defined without touching the legacy `provider.DeviceAuth` contract.
* **Step 8** — deferred activation is implemented in the shared owned flow:
  a validated candidate that meets a busy provider publishes
  `ready_to_activate`, waits for idle, and commits without repeating OAuth;
  the activation deadline aborts and leaves LIVE untouched.
* **Step 13** — `CLIFlow.Wait`/`Kill` are order-independent, idempotent, and
  share one terminal result; a killed flow reports the new
  `ErrFlowCancelled`, and killing an already-finished flow cannot rewrite its
  outcome.
* **`providerauth.StartOwnedFlow`** — the shared engine both providers will
  use: begin, spawn against an empty isolated home, stage, validate, defer,
  commit, with every failure path aborting the transaction.
* **Both adapters** — `codex.CredentialAdapter` and `grok.CredentialAdapter`
  implement `providerauth.Adapter`.
* **Step 4** — `codex.NewCoordinated` and `grok.NewCoordinated` take an optional
  coordinator and live-session-count callback, with no package globals. Grok's
  `Provider` is an alias for the shared ACP agent type, so its coordinator is
  carried by a `CoordinatedProvider` wrapper rather than by new fields on that
  shared struct. Tests assert a production-constructed provider carries no
  coordinator and refuses to start an owned flow.
* **Step 5** — Codex's owned flow runs `login --device-auth` against a private
  empty `CODEX_HOME`, refuses a non-file backend before spawning, and has no
  `confirmDestructive` parameter because an isolated start cannot sign the host
  out. Regression tests cover abandon, child failure, no-credential exit, and a
  child that deletes whatever it finds in its home; all four leave LIVE
  byte-identical.
* **Step 6** — Grok's owned flow runs against a private `GROK_HOME`, preserves
  the macOS sandbox-exec wrapper behind a test seam, and roots its browser
  stubs inside the transaction directory so the coordinator's own cleanup
  removes them. The previous flow wrote stubs to the system temp directory and
  relied on a deferred `RemoveAll` that an orphaned wait would never run.

* **Step 9** — `codex.SetCredentialCoordinated` runs `login --with-api-key`
  inside the transaction with the key on stdin only. Tests assert the key never
  reaches argv, the child environment, the manifest, or error text, and that a
  rejected key leaves LIVE byte-identical. Because Codex keeps one native
  credential, an API-key rotation shares the same CURRENT/PREVIOUS chain as a
  device login.
  *Annotated 2026-11-11 (MADR §16 D38): built, but `SetCredentialCoordinated` had no non-test caller;
  the phone's key write ran `codex login --with-api-key` directly (`codex/auth.go:28-58`), leaving an
  unordered LIVE the coordinator never adopts (§16.2), until P23 step 7 routed it through this function.*
* **Step 10** — `provider.AuthMethodClearer` on both providers. Codex's two
  method ids are aliases for its one credential; Grok's clear independently,
  with tests proving that removing a pasted key does not sign the host out and
  signing out does not delete the key.
* **Step 11** — the logout split from §17.5. For a ChatGPT credential the
  tombstone is made durable and generations are marked known-revoked *before*
  `codex logout` runs against the effective live home, and a revoke that does
  not confirm is reported rather than swallowed. API-key mode keeps the
  clone-and-verify probe, and a failed probe changes neither LIVE nor the
  manifest. An unclassifiable credential takes the revoking path, because
  assuming a credential is safe to rehearse is the failure that matters.
  *Annotated 2026-11-11 (MADR §16 D38): built, but `CoordinatedLogout` had no non-test caller; the
  phone's logout ran `codex logout` directly (`codex/auth.go:61-75`), leaving the revoked generation
  unmarked (§16.2). As built, the revoking path ran `codex logout` after `RecordLogout` had removed
  LIVE, so it would have revoked with no token (§16.2, "the revoke runs with no token"). P23 step 7
  wires it to the phone and revokes from an owner-only copy of LIVE (D38).*
* **Step 12** — presence-only `Configured` on `provider.AuthMethod`. Codex
  derives it from the stored auth mode; Grok reports both methods
  independently. `XAI_API_KEY` is deliberately never a configured method: it
  can make the upstream configured, but the daemon cannot remove its own
  service environment, so offering a removal would do nothing.
* **Step 14** — the sweep: full-repo `go test`, `-race` on every touched
  package, and `make pre-add-check` clean on every changed file.

P18 is complete. Nothing added is reachable from production daemon
construction, so P18's "intentionally dark" property holds and the pre-P20
device-auth path is still what executes.

##### Finding: `grok logout` does not revoke

§17.5 split step 11 by whether a logout revokes, and assigned Grok to the
non-revoking clone-and-verify path on the strength of the F14 analysis being
Codex-specific. That assignment is now independently verified rather than
assumed: in grok-build 1.0.5, `grok logout` calls `run_cli_logout` →
`perform_logout` → `AuthManager::clear()`
(`crates/codegen/xai-grok-shell/src/auth/flow.rs:1100-1170`), which clears the
local file and makes no revoke request. Grok's adapter therefore reports
`Revocable: false`, and Codex's reports `true` only for ChatGPT mode.

#### P17 deviations from the written plan

* Step 1 asked for injection seams "sufficient to inject create/write/sync/
  close/rename/directory-sync failures". Implemented as an unexported `fileOps`
  struct plus an unexported `writeFileAtomic`; the exported
  `WriteFileAtomic` keeps its signature, so no production call site changed.
* Step 7's `StageCandidate` and `ValidateCandidate` are separate calls, as
  written. Staging performs the structural and parse checks and writes the
  immutable PENDING generation; validation runs the provider probe and stamps
  `validated_at`. `Commit` refuses an unvalidated candidate, so the probe is a
  hard precondition for publication rather than an advisory step.
* Step 9's "valid and strictly fresher" comparison is delegated to
  `CredentialMeta.Fresher`, which returns false whenever the two credentials
  have different modes or offer no comparable ordering signal. Freshness is
  therefore never inferred from mcremote-invented timestamps.
* `MarkRevoked` was added beyond the literal step list to carry the D24
  known-revoked grade that §17.5 introduced. P18 step 11 is its first caller.

## 18. Phase P17 — Durable credential transaction core (D21, D23, D25, D26)

Build and fault-test the storage state machine before connecting it to a real
provider. No provider or WebSocket behavior changes in this phase.

**Affected files**

* `internal/fsutil/atomic.go`, `internal/fsutil/atomic_test.go`
* New `internal/providerauth/adapter.go`
* New `internal/providerauth/manifest.go`
* New `internal/providerauth/store.go`
* New `internal/providerauth/transaction.go`
* New `internal/providerauth/transaction_test.go`
* New helper-process fixture/test files under `internal/providerauth/`

**Steps**

1. Strengthen `fsutil.WriteFileAtomic`: when `SyncDir` is requested, return a
   parent-directory open or sync failure. Preserve same-directory temporary
   creation, symlink refusal, requested mode, file sync, close, and rename.
   Add internal operation seams sufficient to inject create/write/sync/close/
   rename/directory-sync failures without changing production call sites.
2. Define a provider adapter consumed by the coordinator: provider ID, effective
   live home/path, pending-home environment, native lock path, bounded candidate
   validator, freshness comparator, and post-write validation probe. Adapter
   methods return metadata or errors only; no logging/string method may expose
   credential bytes. All coordinator entry points accept `context.Context` and
   enforce bounded lock, validation, and activation deadlines.
3. Use this exact on-disk layout; derive every path from the configured data
   directory and the fixed provider ID rather than accepting path fragments from
   callers:

   ```text
   <DataDir>/provider-auth/                         0700
     codex/                                         0700
       manifest.json                                0600, atomic replacement
       transaction.lock                             advisory coordinator lock
       generations/<generation-uuid>.auth           0600, immutable after sync
       pending/<transaction-uuid>/home/              0700, provider-native layout
     grok/                                           0700
       manifest.json
       transaction.lock
       generations/<generation-uuid>.auth
       pending/<transaction-uuid>/home/
   ```

   `CODEX_HOME` or `GROK_HOME` points at the transaction's `home` directory, so
   the candidate is always `home/auth.json`. Use UUIDv4 identifiers, lowercase
   SHA-256 hex fingerprints, and UTC RFC3339Nano timestamps. Do not put the
   effective LIVE path in persistent metadata.
4. Define manifest schema version 1 with provider, state (`idle`, `pending`,
   `committing`, `recovery_required`, `logged_out`), transaction/generation IDs,
   `CURRENT`/`PREVIOUS`/`PENDING` labels, bounded SHA-256 fingerprints, source,
   created/validated timestamps, expected starting LIVE fingerprint, and
   activation deadline. Store no token field, device code, raw path, or child
   output. Reject unknown fields when reading the current schema so a partially
   understood future manifest cannot be mutated by an older binary.
5. Implement `Coordinator` rooted at `<DataDir>/provider-auth`: private root and
   provider directories; immutable, uniquely named `0600` generation payloads;
   a durably replaced `0600` manifest; and a provider-scoped mutation lock.
   Reject symlinks, non-regular files, oversized candidates, wrong owner modes,
   unknown manifest versions, duplicate labels, and path traversal. Acquire
   locks only in this order: coordinator provider lock, then provider-native
   LIVE lock; release in reverse order. Watchers, recovery commands, and every
   mutation use the same order and never call back into a lock-taking method.
6. Implement first-use seeding: under the provider lock, validate an existing
   LIVE and durably create CURRENT before allowing the first managed mutation.
   A missing cold-host LIVE creates no artificial generation.
7. Implement `Begin`, `StageCandidate`, `ValidateCandidate`, `Commit`,
   `Abort`, and `RecordLogout`. Sync the manifest transition before its related
   side effect. After validation, copy the exact candidate bytes into a new
   immutable generation file, sync it and its directory, then label that file
   PENDING; never publish from the child-writable pending home. Commit performs
   the final live fingerprint comparison, writes a same-directory live
   temporary from the immutable PENDING generation, syncs it, renames it, syncs
   the live parent, verifies bytes, then advances labels. It never writes
   directly through a truncate/create path.
8. Retain exactly CURRENT and PREVIOUS plus at most one PENDING transaction.
   Delete superseded payloads only after the new manifest is durable. A logout
   tombstone becomes durable before LIVE and retained payloads are removed.
9. Implement startup recovery by the following exhaustive transition table. A
   fingerprint comparison is made only after regular-file, size, ownership, and
   provider validation checks pass; `absent` is a distinct value, not the hash
   of empty bytes.

   | durable manifest | observed LIVE | deterministic recovery effect |
   | --- | --- | --- |
   | `idle` | equals CURRENT | remain `idle`; remove only stale unlabelled pending directories |
   | `idle` | valid and strictly fresher than CURRENT | reconcile as an autonomous refresh, rotate CURRENT to PREVIOUS, then remain `idle` |
   | `idle` | absent, invalid, older, or unrelated | preserve all generations and enter `recovery_required` |
   | `pending` | equals expected starting LIVE | durably record abort, remove only its pending data, and return to `idle`; a restarted daemon never publishes an ownerless candidate |
   | `pending` | any other value | preserve all evidence and enter `recovery_required` |
   | `committing` | equals candidate/PENDING | finish label rotation, clear transaction fields, and enter `idle` |
   | `committing` | equals expected starting LIVE/old CURRENT | leave LIVE unchanged, discard only the uncommitted candidate after recording abort, and enter `idle` |
   | `committing` | absent or any third value | preserve all evidence and enter `recovery_required` |
   | `logged_out` | absent | remain `logged_out`; ensure retained credential payloads are absent |
   | `logged_out` | equals the tombstone's expected logout fingerprint | finish the journalled removal of LIVE and retained payloads, then remain `logged_out` |
   | `logged_out` | any other present value | preserve the external LIVE file, keep the tombstone, and enter `recovery_required`; never resurrect or delete it automatically |
   | `recovery_required` | any | make no automatic mutation; require the operator command in P19 |

   If no manifest exists, seed a valid existing LIVE as CURRENT and `idle`; if
   LIVE is absent, leave it unmanaged with no payload or logout tombstone. The
   first `Begin` may journal an absent expected LIVE. A malformed manifest never
   triggers reconstruction from filenames.
10. Export sentinel errors `ErrTransactionBusy`, `ErrConflict`,
    `ErrUnsupportedBackend`, `ErrInvalidCandidate`, and `ErrRecoveryRequired`
    from `providerauth`; preserve `provider.ErrAuthBusy` for the distinct
    live-session quiescence case. Wrap with `%w` so callers use `errors.Is`.
    Errors and logs include provider and short operation IDs only—never bytes,
    codes, complete fingerprints, or credential paths.
11. Table-test generation seeding, first/second/later commit retention, logout,
    manifest upgrade/refusal, symlink/mode/size/path rejection, stale start
    fingerprint, lock contention, and every injected operation failure.
12. Add helper-process kill tests at each persisted state transition. Reopen and
    recover to either the old committed state or new committed state; ambiguous
    input must preserve all files and return `recovery_required`.

**Verification**

```bash
go test -count=1 ./internal/fsutil ./internal/providerauth
go test -race -count=1 ./internal/fsutil ./internal/providerauth
```

**Acceptance**

* Every pre-publication failure leaves LIVE byte-identical.
* A post-publication crash is classified from durable state and never guessed.
* Retention and file modes match D23 exactly.
* No secret reaches test names, failure text, logs, or manifest fixtures.

## 19. Phase P18 — Codex and Grok adapters and coordinated mutations (D20–D25)

Build both provider adapters and the owned-flow contract behind constructor
injection. This phase is intentionally dark: production daemon construction
continues to use the existing providers until P20 can activate transaction and
process ownership together. Existing device flows therefore remain reachable
throughout the phased rollout.

**Affected files**

* `internal/provider/auth.go`
* `internal/provider/credstore/credstore.go`, `credstore_test.go`
* `internal/provider/codex/provider.go`, `auth.go`, `device_auth.go` and tests
* `internal/provider/grok/grok.go`, `auth.go`, `device_auth.go` and tests
* `internal/provider/acpagent/acpagent.go` and focused tests
* `internal/providerauth/cli.go`, `cli_test.go`

**Steps**

1. Add deterministic effective-home helpers: Codex uses non-empty `CODEX_HOME`
   else `$HOME/.codex`; Grok uses non-empty `GROK_HOME` else `$HOME/.grok`.
   Derive status, set, clear, live path, lock path, and child environment from
   that one result. Tests set conflicting HOME/provider-home values and prove
   every operation selects the provider home.
2. Detect Codex `cli_auth_credentials_store` from its effective configuration.
   Support only the verified file backend in this repair. Return the typed
   unsupported-backend error for `keyring`, `auto`, or `ephemeral`; do not claim
   protection for an unobservable store.
3. Add an optional `provider.OwnedDeviceAuth` interface without changing the
   legacy `provider.DeviceAuth` contract used by other providers. Its start
   method returns a `provider.DeviceAuthHandle` with immutable `Flow()`, blocking
   `Wait()`, and idempotent `Cancel()` methods. Exactly one internal result is
   shared by `Wait` and `Cancel`; `Cancel` terminates the child/process group,
   waits for it, aborts the transaction, and may be called before or after
   `Wait`. An optional `DeviceAuthUpdateSource` exposes a receive-only channel
   of non-terminal typed state updates; Codex and Grok use it for
   `ready_to_activate`. Codex and Grok implement these new interfaces; P20 makes
   the server require the owned contract for the transactional path.
4. Extend Codex and Grok constructors with an optional coordinator and
   live-session-count callback, without package globals. Tests explicitly
   construct the coordinated variant. Production daemon call sites remain
   unchanged in this phase, and a test asserts that no transactional capability
   is advertised or reachable from production construction yet.
5. Replace the coordinated Codex variant's live-file backup/restore closure.
   Create a private pending home through the coordinator and execute
   `codex login --device-auth` with `CODEX_HOME=<pending-home>`.

   Seeding CURRENT means durably writing the CURRENT **generation** under
   `<DataDir>/provider-auth/codex/generations/` before spawn. It does **not**
   mean placing a credential in the pending home. Per D22 and F14 the pending
   home starts empty of credential material and no code path may copy LIVE into
   it: Codex revokes any stored ChatGPT token server-side before deleting it, so
   a seeded pending home would revoke the live grant while leaving LIVE
   byte-identical and every fingerprint check passing. Assert emptiness at spawn
   in a test rather than relying on construction order.

   Remove the destructive confirmation requirement only on this isolated
   variant — it is sound precisely because an empty pending home gives the child
   nothing to revoke. A start/scan/wait error aborts PENDING and leaves LIVE
   untouched.
6. Run the coordinated Grok variant with `GROK_HOME=<pending-home>`. Preserve
   Linux browser stubs
   and Darwin sandboxing, but root every temporary artifact in the owned
   transaction and clean it through the transaction handle.
7. On zero exit, require a bounded regular `0600` candidate with the provider's
   required JSON fields for the observed auth mode. Run `codex login status` in
   the pending Codex environment. ~~For Grok, launch its ACP transport in the
   pending environment, complete only `initialize` plus cached-token
   authentication, then shut it down before creating a session or prompt.~~
   *Annotated 2026-11-11 (MADR §16 D41): the grok probe was not built and is not added; grok's check is
   the adapter's structural `Validate` (`grok/adapter.go:68`), and `CredentialAdapter.Probe` stays a
   no-op with no `SetProbe` caller. `codex login status` runs and does not discriminate.*
   Bound
   both probes by context deadline and capture only redacted diagnostics. Mark
   PENDING validated only after the probe succeeds.
8. Before publication, call the injected live-session count. If the provider is
   active, retain validated PENDING, emit `ready_to_activate`, and keep the same
   owned handle alive until provider-idle notification, cancellation, or its
   activation deadline. On idle notification, recheck the live-session count,
   acquire the provider's native sibling lock, recompare LIVE, and commit without
   repeating OAuth. Deadline expiry aborts PENDING and terminates as expired;
   cancellation aborts it and terminates as cancelled.
9. Run Codex API-key login through the identical isolated transaction: pass the
   key only on stdin to `codex login --with-api-key` with the pending
   `CODEX_HOME`, validate the resulting candidate and `codex login status`, then
   conditionally publish it. Never pass the key in argv, environment, logs, a
   manifest, or a test failure. Because Codex uses one native `auth.json`, API
   key and device OAuth rotations share the same CURRENT/PREVIOUS chain.
10. Add optional `provider.AuthMethodClearer` with
    `ClearCredentialMethod(ctx, upstreamID, methodID)`. Do not widen
    `AuthWriter`, which would force unrelated providers to implement a semantic
    they do not have. Codex accepts its API-key and device method IDs as aliases
    for clearing the one shared native credential. Grok's API-key method clears
    only `config.toml`; its device method clears only OAuth `auth.json`.
11. Implement native logout, splitting it by whether the invocation revokes.

    The original clone-and-probe design is unsound for Codex ChatGPT
    credentials. `codex logout` revokes the stored refresh token server-side
    before deleting anything (F14), and a clone carries the same token, so the
    "probe" destroys the grant before any fingerprint comparison can decide to
    roll back. A revoking invocation is a point of no return, and zero exit does
    not prove revocation happened — upstream only warns on revoke failure.

    For a Codex ChatGPT credential, treat the invocation as the logout itself,
    not a rehearsal. Under the coordinator and native locks: recheck the LIVE
    fingerprint, write the durable logout tombstone **before** invoking
    `codex logout` against the effective live home, then remove LIVE and all
    generation payloads and mark every affected generation known-revoked (D24).
    A changed LIVE fingerprint aborts before the invocation, which is the only
    point at which aborting is still meaningful. If the process fails after the
    tombstone is synced, startup recovery completes the removal rather than
    resurrecting a grant that may already be revoked.

    For credentials that do not revoke — Codex API-key mode, where
    `revoke_auth_tokens` returns early because `auth_mode != Chatgpt`, and Grok
    — retain the clone-and-verify probe as originally written: clone LIVE into
    an isolated home, invoke `codex logout` or `grok logout` there, require zero
    exit plus absence of the isolated credential, then tombstone and remove. A
    failed probe or changed LIVE fingerprint changes neither LIVE nor the
    manifest. Detect the mode from the live credential before choosing the path,
    and default to the revoking path when the mode cannot be determined.

    Grok API-key set/clear stays outside the OAuth generation chain but holds
    the same provider mutation lock.
12. Add presence-only `Configured` to `provider.AuthMethod`. Determine it from
    mcremote-manageable native files so Grok can truthfully report simultaneous
    `config.toml` API-key and OAuth configuration and Codex can report the auth
    mode stored in its one credential. Do not mark Grok's `XAI_API_KEY`
    environment fallback as a removable configured method: it may make the
    aggregate upstream configured, but this daemon cannot remove its service
    environment. Do not infer method configuration from aggregate status.
13. Make `CLIFlow.Wait` and `Kill` safely callable in any order and ensure both
    observe the same terminal result. The owned handle is responsible for the
    child, transaction directory, activation timer, and one cleanup path.
14. Test Codex and Grok success, cancellation, denial, timeout, malformed output,
    missing/invalid/wrong-mode/oversized candidate, busy activation, native-lock
    contention, start-fingerprint conflict, API-key isolation, method-specific
    clearing, failed logout, and cleanup. Hash LIVE before and after every
    incomplete outcome and require equality. Assert argv, environment, captured
    output, manifests, and error text never contain the supplied API key or
    fixture tokens.

**Verification**

```bash
go test -count=1 ./internal/provider/credstore ./internal/provider/codex \
  ./internal/provider/grok ./internal/provider/acpagent ./internal/providerauth
go test -race -count=1 ./internal/provider/codex ./internal/provider/grok \
  ./internal/provider/acpagent ./internal/providerauth
```

**Acceptance**

* Displaying either provider's code cannot modify LIVE.
* Every incomplete outcome leaves LIVE byte-identical and reaps its process.
* The pending home is empty of credential material at spawn, and no code path
  copies LIVE into a pending home, so an incomplete outcome leaves the live
  grant valid and not merely byte-identical (D22, F14).
* Codex ChatGPT logout writes its durable tombstone before the revoking
  invocation, and affected generations are marked known-revoked rather than
  offered for recovery (D24, D26).
* A successful candidate is independently validated before atomic publication.
* All mcremote credential mutations for a provider share one lock.
* The production daemon still executes its pre-P20 code path, so this commit
  cannot expose a transactional flow without server ownership.

## 20. Phase P19 — Recovery, refresh reconciliation, and operator choice (D23–D26)

Complete and test recovery, reconciliation, watcher, and operator components
without starting them from the production daemon. P20 owns startup ordering and
is the first phase that can activate them.

**Affected files**

* New `internal/providerauth/watch.go`, `watch_test.go`
* `internal/providerauth/transaction.go`, `transaction_test.go`
* `internal/provider/codex/auth.go` and tests
* `internal/provider/grok/auth.go` and tests
* New `internal/cli/auth_recovery.go`, `auth_recovery_test.go`
* `go.mod`, `go.sum` only to make the already-transitive `fsnotify` dependency
  direct if the implementation imports it

**Steps**

1. Implement `Recover(provider)` and `RecoverAll(adapters)` as explicit library
   calls that apply P17's exhaustive state table. Return a result per provider
   rather than failing fast, so one unsupported or recovery-required provider
   cannot hide the other provider's state. Do not invoke these methods from
   daemon construction yet.
2. Implement a watcher object that watches each effective credential parent
   directory, not the credential inode,
   so atomic rename remains visible. Debounce events, defer during a transaction,
   require two stable reads using §17.4's fixed interval/deadline with
   the same fingerprint, then validate and compare freshness under the provider
   lock. Expose explicit `Start(ctx)` and bounded `Close(ctx)` methods; do not
   start goroutines in its constructor.
3. Reconcile startup, pre-mutation, post-commit, and watcher checkpoints through
   one method. A valid monotonic autonomous refresh advances CURRENT/PREVIOUS;
   partial, corrupt, older, deleted, symlinked, unstable, or unsupported-backend
   observations do not alter generations. Startup/pre-mutation checks guarantee
   correctness when watcher events are missed.
4. Make freshness comparison deterministic. Grok compares parsed expiry and
   rotation metadata; a missing/equal/older value is not fresher. Codex compares
   provider-declared token expiry/refresh metadata when present; otherwise an
   unrelated valid LIVE becomes `recovery_required` rather than using file mtime
   as authority. Preserve the documented residual external-writer race: verify
   published bytes and checkpoint a later provably fresher refresh, but do not
   claim exclusion from a CLI that ignores the sibling lock.
5. Surface additive `backup_state` and `recovery_available` metadata through
   `provider.AuthState`; P20 maps it onto the existing auth payload. Use the
   fixed public values `unmanaged`, `current`, `pending`, `logged_out`,
   `recovery_required`, `reauth_required`, and `unsupported`. Values contain no
   paths, hashes, generation IDs, or token metadata.

   `recovery_required` means a restorable generation exists and an operator
   choice is needed. `reauth_required` is the D24 known-revoked case: every
   surviving generation was revoked by a coordinator-initiated Codex action, so
   no restore can succeed and the only path forward is a fresh sign-in.
   `recovery_available` must be false whenever the only candidates are
   known-revoked; a test asserts that a known-revoked manifest never yields
   `recovery_available: true`.
6. Implement, but do not yet register in the production root command, handlers
   for local `mcremote auth-recovery status [provider]` and
   `mcremote auth-recovery choose <provider>
   <live|current|previous|logged-out>` commands. Resolve the same effective
   config/data directory and provider homes as `serve`, construct the same
   adapters, and use bounded coordinator/native lock acquisition so the command
   safely serializes with a running daemon.
7. Define each operator choice exactly: `live` validates and adopts the observed
   LIVE as a new CURRENT; `current` republishes CURRENT; `previous` republishes
   PREVIOUS as a new CURRENT while retaining the displaced CURRENT as PREVIOUS;
   `logged-out` writes the tombstone then removes LIVE and all credential
   generations. Refuse a missing/invalid selection or a manifest not in
   `recovery_required`, preserve all evidence on failure, and post-validate any
   published file before resolving the manifest. Output contains provider,
   public state, and timestamps only. Exit 0 on success, 2 on usage/unknown
   provider or choice, and 3 when validation, locking, or recovery fails.
8. Test write/rename coalescing, missed-event startup recovery, unstable reads,
   valid refresh, rollback refusal, deletion, logout tombstone, transaction
   deferral, bounded watcher shutdown, simultaneous CLI/daemon lock contention,
   and every operator recovery choice. A construction test proves the production
   daemon still starts no coordinator or watcher in this phase.

**Verification**

```bash
go test -count=1 ./internal/providerauth ./internal/provider/codex \
  ./internal/provider/grok ./internal/cli
go test -race -count=1 ./internal/providerauth ./internal/cli
```

**Acceptance**

* A valid external refresh becomes CURRENT without rolling a token backward.
* Missed events are repaired at startup or before the next mutation.
* Unknown deletion and ambiguous recovery never resurrect a credential.
* An operator can resolve every preserved ambiguous state without reading or
  printing credential bytes.
* Recovery/watch components remain dark until P20 wires lifecycle ownership.

## 21. Phase P20 — Owned flow registry and WebSocket lifecycle (D27, D29)

Remove every branch that can drop a started provider process or transaction.

**Affected files**

* `internal/providerauth/registry.go`, `registry_test.go`
* `internal/provider/auth.go` and conformance tests
* `internal/protocol/messages.go`, `provider_auth_test.go`
* `internal/ws/server.go`
* `internal/ws/liveness.go`
* `internal/ws/provider_auth_test.go`
* New focused helper/test files under `internal/ws/` when needed
* `internal/daemon/daemon.go` and startup/shutdown tests
* `internal/cli/root.go` and focused command-registration tests

**Steps**

1. Split registry admission from provider side effects. `Reserve` enforces
   per-device/global limits and returns an owned reservation with idempotent
   `Attach`, `Cancel`, and `Finish`. Failure or abandoned reservation owns no
   child and releases capacity. `Attach` accepts exactly one
   `provider.DeviceAuthHandle`; a second attach fails and cancels the supplied
   handle. Registry keys are `(deviceID, flowID)`, never connection pointers.
2. Give each reservation one owner goroutine and one terminal-result slot. The
   owner forwards optional typed handle updates, calls `Wait` once, recovers a
   panic as a failed terminal result, calls `Finish` once, and remains
   responsible until process reap plus transaction cleanup have completed.
   `ready_to_activate` is non-terminal: ownership and admission remain held.
   `Cancel` signals the handle but does not release capacity before terminal
   cleanup.
3. In `handleStartAuth`, reserve first, derive the flow from `Server.lifeCtx`,
   start the provider transaction, attach it, and start its owner goroutine
   before enqueueing either response frame. Gate terminal delivery on a
   `published` barrier: the owner may finish immediately, but the client sees
   start-result and flow frames before a terminal frame. Failure to enqueue
   either initial frame closes the barrier as failed and cancels through the
   same handle exactly once.
4. Remove `context.WithoutCancel`. Request completion must not cancel the flow,
   but server shutdown must. Derive a child context that carries server lifetime
   plus the flow deadline and is independent only of the individual request.
5. On connection loss, mark the device's flows detached and start the negotiated
   resume-window timer. A successful same-device resume reattaches them; expiry
   cancels them. Explicit `oauth.cancel` remains idempotent and device-scoped.
6. Add `CancelAll` and bounded `WaitAll`. `Server.CloseClients` and daemon
   shutdown cancel and drain device flows before provider shutdown and before
   process exit destroys in-memory ownership.
7. Send terminal results to the owning device's current connection rather than
   retaining the initiating socket pointer. If disconnected, persist terminal
   result metadata in the bounded registry entry until resume expiry and let
   reconnect/auth status report it. Store no child output, device code, or
   credential metadata in that entry.
   *Annotated 2026-11-11 (MADR §16 D42): "auth status report it" was not built for the backup state;
   `authStatePayload` copied neither `backup_state` nor `recovery_available` (`server.go:2332-2348`).
   The status half is P23 step 8; delivering a terminal result on reconnect is
   `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D3.*
8. Add the backward-compatible wire contract in this same activation commit:
   `Caps.ProviderAuthTransactions` encoded as
   `provider_auth_transactions,omitempty`; optional auth result state,
   retryability, `backup_state`, and `recovery_available`; `Configured` on each
   auth method; and optional `method_id` on `ClearCredentialPayload`. Empty
   `method_id` preserves the legacy `AuthWriter.ClearCredential` call. A
   non-empty method ID requires `AuthMethodClearer`; never fall back to an
   aggregate clear that could remove the wrong credential.
9. Use the exact additive result states `completed`, `cancelled`, `expired`,
   `failed`, `conflict`, `ready_to_activate`, `recovery_required`, and
   `unsupported_backend`. Mark only cancelled, expired, conflict, and ordinary
   failed outcomes retryable; `ready_to_activate` is non-terminal and requires
   no retry. Preserve the existing `OK` field for older negotiated clients and
   never overload `agenterr.KindAuth` for coordinator failures.
10. Atomically activate the server-side feature in daemon construction. Create
    the provider registry first, then the session manager so
    `mgr.LiveCountFor` exists before constructing Codex/Grok. Create one
    coordinator, construct the coordinated Codex and Grok providers with that
    callback, register all providers, run `RecoverAll` for enabled file-backed
    adapters, then start watchers, provider prewarm, and WebSocket serving in
    that order. Do not fall back to the destructive legacy flow when an adapter
    is unsupported or recovery-required; leave auth/status reachable and return
    its typed state for mutation attempts.
11. Compose the existing single `session.Manager.OnProviderIdle` hook so it first
    calls the coordinator's bounded `ActivatePending(providerID)` synchronously
    and only after that returns invokes `provider.Controller.OnIdle` for
    prewarm. The coordinator rechecks `LiveCountFor` under its mutation path, so
    a stale notification cannot publish while a new session is active or prewarm
    a process against the old credential.
12. Advertise `ProviderAuthTransactions` only after coordinated providers,
    recovery results, watcher ownership, registry ownership, and shutdown hooks
    have all been installed. Keep `ProviderAuth` independent so hosts and older
    clients retain existing auth reporting even if no transactional adapter is
    enabled.
13. Register P19's `auth-recovery` handlers in the root CLI in this activation
    commit. Their direct invocation constructs no daemon or provider engine and
    uses the same config resolution, adapters, locks, and manifest schema as the
    daemon.
14. Make daemon shutdown ordering explicit and testable: stop accepting new
    requests; close clients; `CancelAll`; bounded `WaitAll`; close and await
    credential watchers; flush session state; shut down providers. Timeout logs
    the stage and retained ownership count but never deletes transaction state.
15. Add WebSocket tests for capacity rejection before spawn, both response-write
   failures, explicit cancel, disconnect+resume, disconnect expiry, duplicate
   cancel/finish, daemon shutdown, owner panic, and terminal delivery after
   reconnect. Add daemon order tests for partial recovery failure, unsupported
   Codex store, capability advertisement, no legacy fallback, and watcher/flow/
   provider shutdown order. Assert exactly one cleanup and zero surviving helper
   processes.

**Verification**

```bash
go test -count=1 ./internal/providerauth ./internal/ws ./internal/daemon ./internal/cli
go test -race -count=1 ./internal/providerauth ./internal/ws ./internal/daemon ./internal/cli
```

**Acceptance**

* No server branch can start a child without installing its owner.
* Shutdown cancels and reaps every flow before returning.
* A transient disconnect is resumable; an abandoned flow expires and cleans up.
* P20 is the first production activation; coordinator, recovery, watchers,
  capability advertisement, and owned flow lifecycle become live together.

## 22. Phase P21 — Truthful phone lifecycle and recovery states (D20, D28)

Update the phone only after the daemon owns every lifecycle path.

**Affected files**

* `apps/mobile/lib/data/protocol/models.dart`
* `apps/mobile/lib/data/ws/mcremote_client.dart`
* `apps/mobile/lib/features/settings/device_flow_sheet.dart`
* `apps/mobile/lib/features/settings/provider_detail_screen.dart`
* `apps/mobile/test/device_flow_sheet_test.dart`
* `apps/mobile/test/provider_detail_screen_test.dart`
* `apps/mobile/test/provider_test_fakes.dart`

**Steps**

1. Decode P20's optional transactional capability, result-state, retryability,
   `backup_state`, `recovery_available`, per-method `configured`, and clear
   `method_id` fields. Default omitted fields to the legacy behavior; unknown
   state strings render as a generic failure and remain available in debug logs
   without including provider output.
2. Remove Codex's destructive confirmation and warning only when the server
   advertises the transactional-flow capability. Copy states that the current
   host credential remains active while sign-in is pending.
3. Render one remove action for each configured method and send its exact
   `method_id`. For Grok, API-key removal and OAuth logout are separate actions;
   for Codex, explain that both displayed methods refer to its one native login
   and either removal signs Codex out. If an older daemon omits per-method state,
   retain the existing aggregate remove action with an empty method ID. If the
   upstream is configured but no method is marked configured, show that the
   credential is externally managed (for example Grok `XAI_API_KEY`) and do not
   offer a removal action the daemon cannot honor.
4. Centralize cancellation in one idempotent controller. Cancel button, Back,
   barrier tap, swipe, route replacement/disposal, and every lifecycle callback
   that can still send must invoke it at most once. Hard process loss relies on
   the server's resume-window expiry.
5. Preserve the flow identifier across reconnect for the advertised resume
   window and rebind to the server's current state. While
   `ready_to_activate`, keep the sheet attached to the owned server flow and
   wait for provider-idle activation; never offer or start a second OAuth
   exchange solely because activation is busy.
6. Render ready-to-activate, conflict, recovery-required, cancelled, expired,
   and completed states distinctly. Recovery availability is presence-only and
   never exposes a host path, fingerprint, or generation ID.
7. Add model/client/widget tests for legacy omission, transactional capability,
   method-specific Grok and Codex removal, every dismissal path, duplicate
   callbacks, reconnect/resume, busy-to-idle automatic activation, and recovery
   state rendering.

**Verification**

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test
```

**Acceptance**

* Every communicable dismissal sends at most one cancel.
* The UI never reports completion before candidate validation and publication.
* Codex and Grok device auth remain available throughout rollout.

## 23. Phase P22 — Full fault, live-contract, and release acceptance (D29)

This phase adds no product behavior. It closes cross-package gaps and performs
the one real-provider acceptance run after all earlier phases are green.

**Affected files**

* `internal/provider/codex/live_auth_test.go` or a new `live_device_auth_test.go`
* `internal/provider/grok/live_auth_test.go`
* Cross-package helper-process tests under `internal/providerauth/` and
  `internal/ws/`
* `README.md` and operator documentation describing backup states and recovery

**Steps**

1. Add `live_codex` coverage in an isolated `CODEX_HOME`: seed an isolated valid
   credential fixture, start/cancel device login, prove upstream deletes only
   the pending credential, and prove the real host file remains byte-identical.
   Record the tested `codex --version` in the test log, but never print or
   snapshot the device code or real token.
2. Extend `live_grok` in an isolated `GROK_HOME` to pin effective-home and native
   lock behavior. Record `grok --version`; keep real host credentials
   byte-identical.
3. Add a cross-package helper that kills the daemon after every journal,
   candidate, sync, rename, and label boundary, restarts it, and verifies the
   D26 convergence table.
4. Run the complete test and hygiene matrix. Live tests spend real tokens and
   run once at acceptance, not in an edit loop.
5. On a configured acceptance host, hash LIVE before code display, during
   polling, and after cancel/timeout/socket loss/forced daemon death. Confirm
   byte identity for both providers. Complete one isolated login per provider,
   validate the resulting session, perform a second rotation, and confirm
   CURRENT/PREVIOUS retention and owner-only modes without printing content.
6. Exercise one busy activation and one explicit logout. Confirm busy retains a
   validated PENDING candidate under the original owned flow, the provider-idle
   hook activates it without another OAuth exchange, method-specific Grok
   API-key removal preserves OAuth, Grok OAuth logout preserves API-key
   configuration, and Codex logout cannot be undone and removes its
   LIVE/generation payloads.
   *Annotated 2026-11-11 (MADR §16 D40, 0245 Q19): steps 5 and 6 moved to
   `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7, runs L1 and L2,
   to be run once against the new design. They are not run under this phase.*
7. Update the MADR and plan from implementation-pending to implemented only
   after all confirmation criteria pass; acceptance recorded on 2026-08-21 must
   remain in the historical status text.

**Verification**

```bash
make test
make race
make preflight
make live-codex
make live-grok
git diff --check
git status --short
```

**Acceptance**

All MADR 0074 §15.9 confirmation items are demonstrated. No secret, device
code, full fingerprint, orphan child, stale temporary, or unbounded generation
appears in output or on disk.

## 24. Repair rollout and rollback

**Rollout**

1. Land P17–P19 dark. Existing production constructors and phone behavior remain
   unchanged, so Codex/Grok device auth stays available on the legacy path while
   the new components are reviewed and tested.
2. Land P20 as one atomic server-side activation. On first start it constructs
   the coordinator, recovers/seeds enabled providers, installs watchers and flow
   ownership, then advertises `provider_auth_transactions`; no intermediate
   binary advertises or invokes only part of that sequence.
3. Start one canary daemon. Startup seeds CURRENT for each valid configured
   file-backed Codex/Grok credential before accepting auth mutations.
4. Verify `backup_state`, modes, retention count, startup recovery, cancellation,
   and one completed rotation per provider.
5. Land P21 after P20 is stable. Older phones continue through the additive wire
   contract; new phones keep legacy warnings/actions when connected to an older
   daemon.
6. Widen only after the canary passes P22.

**Rollback**

* Before P20 activation, revert the current dark phase; retained test
  generations are inert and must not be deleted during binary rollback.
* After activation, prefer a roll-forward fix. Reverting to the old live-store
  Codex flow reintroduces the reported loss bug and is not a safe operational
  rollback.
* If the new coordinator blocks auth mutation, stop new mutations, preserve the
  entire provider-auth directory, use `auth-recovery status/choose` when the
  state requires a choice, verify the selected native credential, then roll
  forward. Ordinary sessions continue on the unchanged LIVE credential.
* Explicit logout payload deletion is irreversible locally; server-side token
  revocation and reauthentication remain the recovery path by design.

## 25. Phase P23 — proposed 2026-11-10, approved 2026-11-11: the MADR's §16 amendment (D30 to D42)

**Status: ~~in progress since 2026-11-11~~ complete 2026-11-12** (step 9: the MADR's 16.5 items 1 to 13 met;
item 14 open, owned by `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's
L1). The owner accepted the MADR's §16 and approved this phase on
2026-11-11 ("Magic 0074"). ~~Nothing here starts before the owner accepts the MADR's §16 and approves this phase.~~ The owner
answered Q-A (D41) and Q-C (D42) on 2026-11-10, resolved Q-B by re-asking Q17 and answering "wire them after all"
(D38), and answered Q-D (D38's fallback, which opens step 7) and the timing (DF: "now, ahead of the redesign"); the
steps below follow those answers. Step 2's D31 half follows the owner's answer (a), of 2026-11-10, to O5 of
`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` (MADR D31); nothing else here
waits. It is its own commit series, after the 0217 pin commit, which landed at `912b9aa1`, and before
[0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md),
which rewrites the same files.

**Conventions.** Every new test fails first on a planted break in a scratch clone, never in the tree, and the record
quotes the failure. Gates run on a scratch clone of `HEAD` with the step's files copied in, because `0179-PLAN-gobble-native-acp-provider.md` P4's
files are unstaged in the tree and are not this phase's: `make pre-add-check FILES="<the step's Go files>"`, `make
test`, `go test -race -count=1 ./...`, `make lint`, `make staticcheck`, `make vet-tags`; Dart `dart format
--output=none --set-exit-if-changed .`, `flutter analyze`, `flutter test`. The Linux test host runs `go test -race
-count=1 ./...` and the Windows test host `make ci-windows` on a scratch copy at steps 3, 7 and 8. The agent stages; the
owner commits with `git commit --no-edit`. No sign-in, no live run, no quota. A difference from this text stops the
phase with a dated entry here.

### Steps

0. **The records, on acceptance.** The MADR's §16 marked accepted with the owner's answers of 2026-11-10 (Q17
   re-asked, Q-A, Q-C, Q-D and DF), dated. In-place annotations, each a dated italic note or a strike-through, quoting
   nothing away: D3, D21 (carried out for codex by D38), D24 (D42), D25 (by Q-A, D41), §15.12's D20 row and "Still
   outstanding"; this PLAN's §17.6 P18 row and its progress note's steps 9 and 11 ("Step 9 —
   `codex.SetCredentialCoordinated` runs `login --with-api-key` inside the transaction", and Step 11's logout split:
   built, and wired to the phone by step 7), step 7's grok probe (§19, not built, D41), P20 step 7 (§21, "let
   reconnect/auth status report it": the status half is step 8 below, the delivery is `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D3), and P22 steps 5
   and 6 (§23, moved to `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7). `0086-MADR-phone-provider-auth-completion.md` D6's SuperGrok row and `0083-MADR-provider-auth-activation-and-layout-gaps.md` D4's
   reason list, a dated note each. `python3 scripts/check_records.py --write-index`; `make check-records
   markdownlint-docs`; the redact preview. **Stage** the records only.
1. **Reproduce 16.2's states first, as the tests step 7 makes pass.** In a scratch clone at `HEAD`,
   `TestPhoneCodexLogoutMarksRevoked` and `TestPhoneCodexKeyWriteCommitsCurrent` (`internal/provider/codex`, with a
   coordinator, codex's adapter and a stub CLI): a coordinator with CURRENT in ChatGPT mode; the phone's clear through
   `ClearCredentialMethod`, then `Recover`, asserting no generation retained, `BackupState` `logged_out` and
   `RecoveryAvailable` false (MADR 16.5 item 9; `RecordLogout` prunes the generations, `transaction.go:585`);
   separately, the phone's key write through `SetCredential`, then `Recover`, asserting CURRENT
   holds the new key and no `recovery_required`. Both fail at `HEAD`, where the paths are direct; the record quotes
   each failure and the state it found. They are committed with step 7.
2. **D30 and D31, kilo and opencode** (`internal/provider/httpagent/authcatalog.go`, `deviceauth.go`,
   `internal/provider/kilo/auth.go`, `internal/provider/opencode/acp_auth.go`, their tests):
   * `browserOAuthMarkers` and the label branch of `ClassifyCatalogMethod` removed; `oauth` entries `oauth_device`;
   * `oauthType` removed; opencode `oauth` methods `oauth_device`;
   * **D31, under the owner's answer (a) to `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`
     O5** (MADR D31; answered 2026-11-10): `methodSlug(label string) string` *(built as `methodSlug(typ, label string) string`, which falls back to the type for a
     label with no letter or digit; see the step 2 execution entry)*; `FetchAuthMethods` mints `<upstream>:<slug>`; `EngineMethodIndex` becomes
     `ResolveEngineMethod(ctx, api, upstreamID, methodID) (int, error)`, re-reading the catalog; `VerifyAPIKeyMethod`
     uses it;
   * tests, each seen failing: `TestNoLabelDecidesTheFlow` (a method labelled "Login with a browser" is `oauth_device`;
     its loopback authorize URL is refused at start with "signs in through a browser on the host"; planted: the marker
     list restored); `TestKiloMethodIDsSurviveAReorder` (the catalog reordered between listing and start starts the
     chosen method; planted: index ids); `TestAmbiguousSlugIsRefused` (two methods with one slug, `method_unsupported`;
     planted: first match taken). `TestAuthStatusSplitsBrowserFromDeviceOAuth` (`kilo/auth_test.go:136`),
     `TestSuperGrokSubscriptionClassifiesBrowser` and `TestHeadlessVPSClassifiesDevice`
     (`httpagent/authcatalog_honesty_test.go:45`, `:52`) are rewritten to the new rule, and named here with why.
   **Stage.**
3. **D33 and D34, the owned lifecycle** (`internal/providerauth/ownedflow.go`, `reservation.go`, `registry.go`,
   `internal/provider/codex/owned_device_auth.go`, `internal/provider/grok/owned_device_auth.go`,
   `internal/ws/provider_auth_owned.go`, `server.go`, their tests):
   * `OwnedFlowConfig.CodeLifetime`; codex 15 minutes, grok `grokDeviceExpirySeconds`; `DeviceFlowInfo.ExpiresIn` from
     it; `Reservation.Attach` arms a timer at `min(attach + lifetime, reserve + DefaultTTL)` that cancels unless the
     flow has published `ready_to_activate`, after which `ActivationGrace` bounds it; the terminal state `expired`.
     The timer is a new field, `deadline *time.Timer`, never `Reservation.timer` (`reservation.go:87`), which the
     resume window owns: `detach` replaces it (`:274-277`) and `reattach` clears it (`:285-288`). `finish` stops both
     (`:203-205`'s place);
   * `deviceAuthWired` returns true for a `provider.OwnedDeviceAuth` before asking `DeviceAuthCapable`;
   * tests, each seen failing: `TestOwnedFlowReportsTheCodesLifetime` (grok 600; planted: the reservation's
     `ExpiresIn`); `TestOwnedFlowExpiresAtItsDeadline` (planted: no timer); `TestReadyToActivateOutlivesTheCode`
     (planted: the timer not disarmed); `TestDeadlineSurvivesDetachAndResume` (detach, reattach within the window, and
     the deadline still cancels the reservation, `expired`; planted: the deadline kept in the shared `timer` field);
     `TestDeviceAuthWiredForOwnedFlows` (an owned provider with no legacy hook; planted: today's rule).
   **Stage.**
4. **D36, error codes** (`internal/protocol/errors.go`, `internal/ws/server.go`, `auth_err_code_test.go`,
   `apps/mobile/lib/data/ws/mc_exception.dart`, `provider_detail_screen.dart`, `device_flow_sheet.dart`, their tests):
   the five codes, registered; `authErrCode`'s cases; the phone's copy; the result sheet reading `state` and
   `retryable`. Tests, each seen failing: `TestCoordinatorSentinelsHaveTheirOwnCodes` (planted: the default branch);
   the existing registry test (planted: a code unregistered); a widget test per `state` (planted: `ok` read alone).
   **Stage.**
5. **D32 and D35, dead code and the phone's codex dialog** (`internal/protocol/messages.go`, `errors.go`,
   `internal/provider/auth.go`, `credstore/write.go`, `providerauth/registry.go`, `codex/device_auth.go` and its test,
   `codex/provider.go`, `kilo/auth.go`, `httpagent/authcatalog.go`, `opencode/acp_auth.go`, `gobble/auth.go`,
   `ws/method_availability_test.go`, `internal/provider/auth_conformance_test.go`, `apps/mobile/lib/features/settings/provider_detail_screen.dart`,
   `device_flow_sheet.dart`, `mc_exception.dart`, their tests): every name of D35 removed; the two comments corrected;
   `provider.AuthReasonHostOAuth` defined once and used by the three packages, `protocol.AuthReasonHostOAuth` set from
   it; the destructive dialog and `confirmDestructive: destructive` removed from `_runDeviceSignIn`. Removing codex's legacy `StartDeviceAuth`
   (`codex/device_auth.go:40`) makes codex no longer a `provider.DeviceAuth`, so `TestProviderAuthInterfaceConformance`'s
   codex row (`auth_conformance_test.go:51-53`, `wantDeviceOAuth: true`) changes to false, and the test gains a
   `wantOwnedDeviceAuth` column, true for codex and grok, so the owned flow stays held; named here with why.
   *Note 2026-11-11: 0179 P4 lands before this step (owner's order), so `auth_conformance_test.go` will already hold
   gobble's case; its row gets `wantOwnedDeviceAuth: false`, since gobble is not an `OwnedDeviceAuth`.* Assertions by
   search: `git grep -n "keyring_managed\|AuthQuota\|AuthStatusQuota\|ZeroString\|CancelDevice\|ErrAuthConfirmRequired\|confirm_required\|confirmDestructiveSignIn\|\"host_oauth\""`
   *(2026-11-11, deviation 3: run without `confirm_required`, which is live on the codex surface, and with the one
   definition of `provider.AuthReasonHostOAuth` expected)* finds nothing outside `docs/`; `rg -n "providerId == 'codex'" apps/mobile/lib` finds the removal copy alone.
   **Stage.**
6. **D37, grok's aggregate status** (`internal/provider/grok/auth.go`, its test): configured from `configuredMethods`
   or `XAI_API_KEY`. Test `TestGrokStubAuthJSONIsNotConfigured` (a `{}` file is `missing`; planted: `FileExists`
   restored). **Stage.**
7. **D38, the wiring (Q17 re-asked; Q-D answered (a), 2026-11-10)** (`internal/provider/codex/auth.go`, `logout.go`,
   `apikey_test.go`, `logout_test.go`, and the step 1 tests):
   * `codex/auth.go`: the direct body of `ClearCredential` (`:61-75`) moves to an unexported `runLogout(ctx)`;
     `ClearCredential` calls `CoordinatedLogout` when `p.coord != nil`, else `runLogout`; `SetCredential` (`:28-58`)
     calls `SetCredentialCoordinated` when `p.coord != nil`, else its direct body, unchanged, moved to an unexported
     `setCredentialDirect`;
   * `codex/logout.go`, `CoordinatedLogout`'s revoking path (MADR D38, "revokes from a copy"): before `MarkRevoked`,
     read LIVE and write it to `<tmp>/auth.json` in a new `os.MkdirTemp` home, `0700`, the file `0600`, as
     `verifyLogoutOnClone` does (`:112-127`), with a deferred `os.RemoveAll(tmp)`; LIVE absent skips the revoke; then
     `MarkRevoked` and `RecordLogout` unchanged; then `runLogoutIn(ctx, tmp)` (`:138`) in place of
     `p.ClearCredential(ctx, "")` (`:89`), whose error keeps today's wrap. `CoordinatedLogout` no longer calls
     `ClearCredential`, so the routing cannot recurse. The non-revoking path is unchanged;
   * **Q-D's fallback:** `ClearCredential` and `SetCredential` call the coordinated function; when it returns an
     error matching `providerauth.ErrUnsupportedBackend` from its first check, before any mutation
     (`SetCredentialCoordinated`'s `CheckBackend`, `logout.go:175`; `CoordinatedLogout`'s `MarkRevoked` on a
     `StateExternal` manifest, `transaction.go:541-546`), they run `runLogout` or `setCredentialDirect` instead. The
     coordinated functions mark that early refusal so the caller can tell it from a later one (a wrapped sentinel,
     `errEarlyUnsupportedBackend`, unexported); a later `ErrUnsupportedBackend` (`RecordLogout`'s, `:558-560`, once
     `MarkRevoked` has run) is returned as it is, and D36 maps it to `unsupported_backend`;
   * tests, each seen failing on a planted break:
     * step 1's two (planted: the routing removed);
     * `TestCoordinatedLogoutRevokesWithTheToken` (`logout_test.go`): a stub `codex` whose `logout` exits 1 unless
       `$CODEX_HOME/auth.json` exists and holds the planted refresh token, and journals `$CODEX_HOME`; the coordinated
       ChatGPT logout succeeds, the journal shows the tombstone first (as `TestChatGPTLogoutTombstonesBeforeRevoking`
       does) and `logout` run in a home other than the daemon's, and that temporary home is gone afterwards.
       **Planted break: today's order**, `RecordLogout` then `p.ClearCredential` in the daemon's `CODEX_HOME`: the stub
       finds no `auth.json` and fails, and `CoordinatedLogout` returns "revoke did not confirm". The existing stub
       (`logout_test.go:22-25`), which never checks, is left as it is for the tests that use it;
     * `TestCoordinatedLogoutRemovesTheCopy`: the temporary home is removed when `runLogoutIn` fails, and when
       `RecordLogout` fails after the copy (planted: the `RemoveAll` not deferred);
     * `TestClearCredentialDoesNotRecurse` (a coordinated ChatGPT logout returns within the 30 s bound with `codex
       logout` run once; planted: `CoordinatedLogout` calling `ClearCredential` again, which the test's timeout
       catches);
     * `TestUnprotectableCodexCredential`: on a `StateExternal` manifest, the phone's clear runs `codex logout` in the
       daemon's home (the direct path) and the key write `codex login --with-api-key` there, each once, and the
       manifest is byte-identical before and after; with `CheckBackend` refusing (a stub whose `doctor` reports an
       unsupported store), the key write takes the direct path too (planted: the fallback removed, so both return
       `unsupported_backend`);
     * `TestLateUnsupportedBackendIsReturned`: an `ErrUnsupportedBackend` from `RecordLogout` after `MarkRevoked` ran
       is returned and nothing falls back (planted: the fallback taken on any `ErrUnsupportedBackend`);
     * `TestCoordinatedLogoutNeedsACoordinator` and `TestSetCredential*` without a coordinator pass unchanged.
   * The live check that the grant is revoked at OpenAI, a refresh with the old token refused, is owner-run and
     needs a sign-in, so it is not this phase's ("no live run"): it is
     `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1 (MADR 16.5 item 14).
   **Stage.**
8. **D42, recovery state on the status payload** (`internal/protocol/messages.go`, `internal/ws/server.go`,
   `internal/provider/grok/owned_device_auth.go` or a new `grok/status.go`, `apps/mobile/lib/data/protocol/models.dart`,
   `apps/mobile/lib/features/settings/provider_detail_screen.dart`, their tests):
   * `ProviderAuthPayload.BackupState` (`json:"backup_state,omitempty"`) and `RecoveryAvailable`
     (`json:"recovery_available,omitempty"`); `authStatePayload` copies them;
   * grok's `CoordinatedProvider` reports them from `c.coord`, by the rule of codex's `backupProjection`
     (`codex/logout.go:230`);
   * the phone's provider auth model reads them; the provider screen shows the backup state and, when recovery is
     available, "A backup can be restored on the host with `mcremote auth-recovery`";
   * tests, each seen failing: `TestStatusCarriesRecoveryState` (a coordinated codex and a coordinated grok provider:
     both members present; an uncoordinated one: neither, byte-identical to today's frame; planted: the copy
     removed); `TestGrokStatusProjectsBackup` (planted: grok's projection returning empty); a widget test for each
     backup state and for `recovery_available` (planted: the field not read).
   **Stage.**
9. **Close-out.** The MADR's §16.5 items 1 to 13, each with its evidence (item 14 is owner-run, recorded by
   `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7); this phase's status; P22's acceptance row
   annotated as moved (D40).

### Verification

```bash
make pre-add-check FILES="<the step's Go files>"
make test
go test -race -count=1 ./...
make lint staticcheck vet-tags preflight
cd apps/mobile && dart format --output=none --set-exit-if-changed . && flutter analyze && flutter test
make ci-windows                     # the Windows test host, steps 3, 7 and 8
python3 scripts/check_records.py --check-all
python3 ~/.global-git-hooks/github-disclosure.py redact
```

### Rollout and rollback

* Each step is one commit, in this order. Steps 2 to 8 change the wire only by the codes of step 4, the removed
  `keyring_managed` and `quota`, which nothing sends, and step 8's two additive members; an older phone shows a new
  code's message text and ignores the two members.
* Step 2's id change makes a phone's cached kilo method ids stale; the phone re-reads the catalog when it opens the
  sheet (`internal/provider/httpagent/authcatalog.go:117-121`, `FetchAuthMethods`'s comment), so nothing is stored to
  migrate.
* Rollback: revert newest first. Step 7's coordinated writes leave coordinator state (a committed CURRENT, revoked
  generations, a tombstone); reverting it leaves that state, which the older code reads (the manifest's states are
  unchanged).

### Execution record

* **Step 0, 2026-11-11 — the records, on acceptance.** The owner accepted the MADR's §16 and approved this phase
  ("Magic 0074"). Annotated in place, each a dated italic note or a strike-through, nothing rewritten: the MADR's
  status and date cells, D3 (D39), D21 (D38), D24 (D42), D25 (D41, the grok probe struck), §15.12's D20 row (D39, the
  capability clause struck) and "Still outstanding" (D40), §16's heading, status and closing note; this PLAN's status
  bullet, §17.6's P18 row, the P18 progress note's steps 9 and 11, §19 step 7 (the grok probe struck), §21 step 7, §23
  steps 5 and 6, and §25's heading and status; `0086-MADR-phone-provider-auth-completion.md` D6's SuperGrok row
  (superseded by D30); `0083-MADR-provider-auth-activation-and-layout-gaps.md` D4's reason list (D35). Checks:
  `python3 scripts/check_records.py --check-all` exit 0, no output; `make markdownlint-docs` "60 files, 0 issues";
  `python3 ~/.global-git-hooks/github-disclosure.py redact` "would edit 0 file(s)". `--write-index` wanted to add only
  the four rows for the 0246 and 0247 drafts, which are not committed, so those rows were dropped again and the index
  is unchanged; they are added when those records are accepted. Staged: the four records above, nothing else. Commit:
  the owner's.
* **Step 1, 2026-11-11 — 16.2's states reproduced.** `TestPhoneCodexLogoutMarksRevoked` and
  `TestPhoneCodexKeyWriteCommitsCurrent` (`internal/provider/codex/phone_paths_test.go`, held in the scratchpad until
  step 7 commits them) built with a coordinator seeded from a ChatGPT credential, codex's adapter and a stub `codex`
  whose `logout` removes `$CODEX_HOME/auth.json` and whose `login --with-api-key` writes the key from stdin. Run in a
  scratch clone of `bcde74ab` (step 0's commit), both fail on the direct paths, as 16.2 reads them:

  ```text
  --- FAIL: TestPhoneCodexLogoutMarksRevoked (0.51s)
      phone_paths_test.go:111: after a phone logout and a restart: state=recovery_required backup=recovery_required recovery_available=true generations=1; want logged_out, logged_out, false, 0 (the revoked generation must not be retained or offered)
  --- FAIL: TestPhoneCodexKeyWriteCommitsCurrent (0.47s)
      phone_paths_test.go:143: after a phone key write and a restart: state=recovery_required, CURRENT holds the new key=false; want a state other than recovery_required and CURRENT holding the key
  ```

  So after a phone logout the next start holds the revoked ChatGPT generation and offers it (`recovery_available=true`),
  and after a phone key write the next start cannot adopt the unordered LIVE and asks for recovery. Step 7 makes both
  pass.
* **Step 2, 2026-11-11 — D30 and D31.** `internal/provider/httpagent/authcatalog.go`: `browserOAuthMarkers` and the
  label branch gone; `ClassifyCatalogMethod(typ)` lists every `oauth` entry `oauth_device`; `methodSlug` mints
  `<upstream>:<label slug>` (D31, refined: no leading or trailing `-`, an empty slug takes the type; the MADR carries
  the note); `resolveMethod` finds a minted id in one upstream's current methods and refuses none or more than one, or
  an index id, as `ErrAuthMethodUnsupported`; `VerifyAPIKeyMethod` resolves through it. `deviceauth.go`:
  `EngineMethodIndex` replaced by `ResolveEngineMethod(ctx, api, upstreamID, methodID)`, which re-reads
  `GET /provider/auth` inside the start timeout. `internal/provider/opencode/acp_auth.go`: `oauthType` gone; every
  `oauth` method `oauth_device`. `internal/provider/kilo/auth.go` needed no change (its `SetCredentialFile` checks the
  upstream prefix only). Tests: `httpagent/methodid_test.go` adds `TestNoLabelDecidesTheFlow`,
  `TestKiloMethodIDsSurviveAReorder`, `TestAmbiguousSlugIsRefused`, `TestIndexMethodIDsAreRefused` and
  `TestMethodSlug`; `TestSuperGrokSubscriptionClassifiesBrowser` and `TestHeadlessVPSClassifiesDevice` become
  `TestOAuthEntriesClassifyDeviceWhateverTheLabel` (the first held the label heuristic D30 removes, the second its
  complement); kilo's `TestAuthStatusSplitsBrowserFromDeviceOAuth` becomes `TestAuthStatusListsEveryOAuthMethodAsDevice`
  (asserts the fixture's three openai methods by slug id and type, and no `oauth_browser` anywhere);
  `TestMethodIndexOf` becomes `TestResolveEngineMethod` against a served catalog, and `deviceAPI` serves
  `/provider/auth` because the start now reads it; `TestSetCredentialCarriesInputsAndGuardsMethod` uses slug ids and
  gains the index-id refusal; opencode's `TestACPAuthStatusIsSmall` row for `oauthy` wants two device methods; the live
  `TestLiveKiloCatalogMarksXaiMethods` wants two startable device methods and
  `TestLiveKiloGatewayDeviceDoesNotFalseComplete` starts with an empty id (the first method), since the live label may
  change between engine versions. Each new test was seen failing on its planted
  break in the scratch clone (`step2_plants.log`): the marker list restored —
  `a method labelled "Login with a browser" is listed {… Type:oauth_browser …}, want a startable oauth_device`; index ids
  — `listed id = "xai:1"`; first match taken — `an ambiguous slug resolved: err=<nil>, want ErrAuthMethodUnsupported`;
  all three pass clean. Gates on the scratch clone of `bcde74ab` with the step's nine Go files copied in:
  `make pre-add-check FILES=…` "9 file(s) clean (gofmt, golint, govulncheck)"; `make test` 50 packages ok;
  `go test -race -count=1 ./...` 50 packages ok, no FAIL; `make lint` clean; `make staticcheck` clean (56 ignored
  findings, all the known acp-go-sdk2 unstable-schema names); `make vet-tags` live_grok, live_kilo, live_opencode ok;
  `flutter analyze` "No issues found"; `flutter test` "+1523 ~3: All tests passed". `dart format
  --set-exit-if-changed` reported two files changed on its first run in the clone, before `flutter analyze` had resolved
  the clone's packages (every file warned "Failed to resolve package URI package:flutter_lints/flutter.yaml"); re-run
  after resolution, and in the tree, it reports "231 files (0 changed)". No Dart file changed in this step. Staged: the
  nine Go files and the two 0074 records (the MADR's D31 note, this entry). Commit: the owner's.
* **Step 3, 2026-11-11 — D33 and D34, the owned lifecycle.** `providerauth/ownedflow.go`: `OwnedFlowConfig.CodeLifetime`,
  and `DeviceFlowInfo.ExpiresIn` from it. `providerauth/reservation.go`: `Attach` arms `deadline`, a timer of its own
  (never `timer`, the resume window's), at the code's lifetime capped at the reservation's `DefaultTTL`, and sets the
  reported `expires_in` to it; `forward` re-arms it at `ActivationGrace` when the flow publishes `ready_to_activate`;
  `expire` marks the flow expired and cancels it through the handle; `finish` stops both timers and reports `expired`
  for a flow the deadline cancelled. `codex/owned_device_auth.go` passes 15 minutes, `grok/owned_device_auth.go`
  `grokDeviceExpirySeconds`. `ws/server.go`: `deviceAuthWired` returns true for a `provider.OwnedDeviceAuth` before
  asking `DeviceAuthCapable`. Tests, in `providerauth/deadline_test.go`, `grok` and `codex` `owned_device_auth_test.go`,
  `ws/device_auth_wired_test.go`; `reservation_test.go`'s stub gained `cancelErr` so a cancelled stub returns
  `ErrFlowCancelled` as a real handle does. Each was seen failing on its planted break in a scratch clone
  (`step3_plants.log`):
  * `TestOwnedFlowReportsTheCodesLifetime`, the cap line removed: `the reservation reports expires_in 899, want the
    code's 600, not DefaultTTL's 900`;
  * `TestOwnedGrokFlowReportsItsCodesLifetime`, `CodeLifetime` zeroed: `expires_in = 0, want grok's 600` (the first
    plant, a deleted line, failed the build on an unused import and was redone); `TestOwnedCodexFlowReportsItsCodesLifetime`
    holds codex's 900 the same way;
  * `TestOwnedFlowExpiresAtItsDeadline`, no timer: `a flow outlived its code: nothing enforced the deadline`;
  * `TestReadyToActivateOutlivesTheCode`, the timer not disarmed: `a ready_to_activate flow was ended at its code's
    expiry, state "expired"`;
  * `TestDeadlineSurvivesDetachAndResume`, the deadline in the shared `timer`: `a resumed flow outlived its code: the
    deadline was lost with the resume window's timer`;
  * `TestDeviceAuthWiredForOwnedFlows`, today's rule: `owned flow, no legacy hook: deviceAuthWired = false, want true`
    and the same for a legacy hook that says no;
  * `TestUpdateThenFinishDoesNotRace`, the unfixed code (deviation 2): `WARNING: DATA RACE` then `panic: send on
    closed channel`.
  All pass clean. Gates, on a scratch clone of `14200b6e` with the step's ten Go files copied in: `make pre-add-check
  FILES=…` "10 file(s) clean"; `make test` and `go test -race -count=1 ./...` 50 packages ok each; `make lint`,
  `make staticcheck`, `make vet-tags` clean. The Linux test host, `go test -race -count=1 ./...`: 50 packages ok, no
  failure or race. The Windows test host, `make ci-windows`: "ALL SELECTED CHECKS PASSED". Two earlier host runs were
  void and are not counted: the first package carried macOS `._*` metadata files that the repo's own parsers read, and
  the second left out `.git`, which the home-path guard reads; both failures were of the package, not the code, and
  the reruns pack neither. No Dart file changed, so the Dart gates did not run. Staged: the step's ten Go files and
  the two 0074 records. Commit: the owner's.
* **Deviation 2, 2026-11-11 — a data race in `Reservation`, found by `-race` while writing step 3's tests.** Evidence:
  `internal/providerauth/reservation.go`, `forward` sending on `r.updates` while `finish` closes it; at `bcde74ab`,
  with none of step 3's code, a probe that sends one update and ends the flow fails under `-race` with `WARNING: DATA
  RACE` and `panic: send on closed channel`. Pre-existing. The window is narrow in production (`ready_to_activate`
  followed at once by completion) and its effect is a daemon panic. Resolutions put to the owner: fix it inside step 3
  (recommended); fix it in a commit of its own first; leave it and remove the update from the test, which was refused
  as a workaround. Decision: the owner chose to fix it inside step 3 ("fix the race inside step 3", 2026-11-11, in this
  session, after a relayed answer from the coordinator session was held for that confirmation). Added to the step's
  scope: `reservation.go`'s `forward` and `finish`, and `TestUpdateThenFinishDoesNotRace`. The MADR's D33 carries the
  amendment.
* **Step 4, 2026-11-11 — D36, error codes.** Run after `0217-PLAN-update-every-pin-to-its-current-release.md` P3j
  landed as `83aac453`, on the owner's word to resume. `internal/protocol/errors.go`: `ErrProviderAuthBusy`
  (`provider_auth_busy`), `ErrCredentialConflict` (`credential_conflict`), `ErrUnsupportedBackend`
  (`unsupported_backend`), `ErrRecoveryRequired` (`recovery_required`) and `ErrTooManySignIns` (`too_many_sign_ins`),
  each commented and registered in `ErrorCodes()`. `internal/ws/server.go`: `authErrCode` maps `providerauth`'s
  `ErrTransactionBusy`, `ErrConflict`, `ErrUnsupportedBackend`, `ErrRecoveryRequired` and `ErrTooManyFlows` to them,
  before the `credential_failed` default, wrapped or not. `docs/guides/protocol-v1.md`: a paragraph for the five and
  for a device-flow result's `state` and `retryable`; it is not on the step's file list, and
  `TestErrorCodesAreDocumented` requires it. The phone: `mc_exception.dart`'s `friendlyOpError` gives each of the five,
  `unsupported` and `credential_failed` a sentence (`method_unsupported` had one); `device_flow_sheet.dart` gains
  `DeviceFlowOutcome` (`ok`, `state`, `retryable`, `error`, with `message` and `succeeded`), the sheet's `result` is a
  `Future<DeviceFlowOutcome>`, it renders each state (`completed`, `cancelled`, `expired`, `conflict`,
  `recovery_required`, `unsupported_backend`, `failed`) in its own words and shows "You can start the sign-in again."
  for a failed, retryable result; `provider_detail_screen.dart` builds the outcome from the result frame with
  `DeviceFlowOutcome.fromJson`. A frame with no `state` (an older daemon) falls back to `ok` and `error`. Tests:
  `TestCoordinatorSentinelsHaveTheirOwnCodes` (`ws/auth_err_code_test.go`, wrapped and bare); the registry and
  documentation tests existing; in `device_flow_sheet_test.dart`, a widget test per state, one for `completed` showing no
  hint and one for the fallback; in `friendly_op_error_test.dart`, one per code. Changed tests: `device_flow_sheet_test.dart`
  and `device_flow_lifecycle_test.dart` pass a `DeviceFlowOutcome` where they passed `Future<String?>`;
  `friendly_op_error_test.dart`'s "credential_failed falls through to the daemon message" becomes "credential_failed
  keeps the daemon detail inside its own sentence", since D36 gives `credential_failed` copy of its own and the detail
  stays in it. Each new test was seen failing on its planted break in a scratch clone of `83aac453`
  (`step4_plants.log`):
  * the five `authErrCode` cases removed: `transaction busy (wrapped=false): code = "credential_failed", want
    "provider_auth_busy"` and the same for the other four, bare and wrapped;
  * a code left unregistered: `ErrTooManySignIns ("too_many_sign_ins") is declared in errors.go but not in
    ErrorCodes()`;
  * a code left undocumented: `error code "too_many_sign_ins" is registered but not documented in
    ../../docs/guides/protocol-v1.md`;
  * the sheet reading `ok` alone (the state switch emptied): `Found 0 widgets with text "Sign-in cancelled."` and the
    same for `expired` and the other ok:false states;
  * `friendlyOpError` losing `recovery_required`: `Expected: contains 'auth-recovery'  Actual: 'raw daemon text'`.
  All pass clean. Gates, on the scratch clone with the step's ten files copied in: `make pre-add-check FILES=…` "3
  file(s) clean"; `make test` and `go test -race -count=1 ./...` 50 packages ok each; `make lint`, `make staticcheck`,
  `make vet-tags` clean; `dart format --set-exit-if-changed` "231 files (0 changed)"; `flutter analyze` "No issues
  found"; `flutter test` "+1536 ~3: All tests passed". The Linux and Windows hosts are not run for this step (the
  conventions name steps 3, 7 and 8). Staged: the step's ten files and this record. Commit: the owner's.
* **Step 5, 2026-11-11 — D32 and D35, dead code and the phone's codex dialog.** Run on the owner's answer to deviation 3
  and after step 4 landed as `ea18ceff`. Go: `provider.ErrAuthConfirmRequired`, `provider.AuthQuota`,
  `protocol.AuthStatusQuota`, `protocol.ErrKeyringManaged` (constant and registry entry),
  `protocol.AuthReasonKeyringManaged`, `credstore.ZeroString`, `Registry.CancelDevice` and `TestCancelDevice`, codex's
  legacy `StartDeviceAuth` and all of `codex/device_auth_test.go`, and `authErrCode`'s `ErrAuthConfirmRequired` case are
  gone; the two stale comments (`codex/provider.go`, `kilo/auth.go`) say what is true, a third (`codex/auth.go`) and one
  test comment point at the owned flow, and `provider.DeviceAuth`'s doc no longer promises `ErrAuthConfirmRequired`.
  `provider.AuthReasonHostOAuth` is defined once; `protocol.AuthReasonHostOAuth` is set from it, and `httpagent`,
  `opencode` and `gobble` use it in place of their own literals (the gobble test too). `protocol.ErrConfirmRequired`
  stays, comment corrected. Dart: the destructive dialog and `confirmDestructive: destructive` are gone from
  `_runDeviceSignIn`, `confirmDestructiveSignIn` is removed from `device_flow_sheet.dart`, `friendlyOpError` and
  `upstream_catalog_sheet.dart` lose their `keyring_managed` branches, and `models.dart`'s reason list is corrected.
  `docs/guides/protocol-v1.md` loses its `keyring_managed` entries and says what `confirm_required` is. Changed tests,
  each with why: `TestProviderAuthInterfaceConformance`'s codex row is `wantDeviceOAuth: false`, since removing the
  legacy flow ends codex's `provider.DeviceAuth`, and the table gains `wantOwnedDeviceAuth`, true for codex and grok
  (grok's row is built on its coordinated wrapper, which embeds the plain provider so every other column reads the
  same) and false for the rest, gobble included (0179 P4's case); `TestProductionCodexExposesNoTransaction` read "the
  legacy contract must still be there for the pre-P20 path" and now holds that codex has none; `method_availability_test.go`
  and three Dart tests used `keyring_managed` as a stand-in reason and use `host_oauth`, the real one, and
  `provider_detail_screen_test.dart`'s end-to-end error test uses `recovery_required` (named above, since the
  `auth-recovery` sentence is the visible one); the destructive-confirmation widget group and the `keyring_managed` copy
  test are deleted with what they tested. Each changed or new assertion was seen failing on its planted break in a
  scratch clone of `ea18ceff` (`step5_plants.log`):
  * the codex row as before: `DeviceAuth: got false, want true`;
  * grok's row on the plain provider: `OwnedDeviceAuth: got false, want true (MADR 0074 D35)`;
  * gobble's row claiming an owned flow: the same message, in the gobble subtest;
  * codex with a legacy `StartDeviceAuth` again: `codex has a legacy DeviceAuth again; it signs the host out at the start`;
  * the search, with `CancelDevice` reintroduced and with a second `"host_oauth"` literal: one hit each, at the planted line.
  Assertions by search, on the final tree: the narrowed pattern finds nothing outside `docs/`, except the one definition
  of `provider.AuthReasonHostOAuth`; `rg -n "providerId == 'codex'" apps/mobile/lib` finds the removal copy alone
  (`provider_detail_screen.dart:702`). Gates, on a scratch clone of `ea18ceff` with the step's 31 files in place:
  `make pre-add-check FILES=…` "19 file(s) clean"; `make test` and `go test -race -count=1 ./...` 50 packages ok each,
  no failure; `make lint`, `make staticcheck`, `make vet-tags` (nine tags) clean; `dart format --set-exit-if-changed`
  "231 files (0 changed)"; `flutter analyze` "No issues found"; `flutter test` "+1532 ~3: All tests passed". The Linux
  and Windows hosts are not run for this step (the conventions name steps 3, 7 and 8). While editing, a cut in
  `device_flow_sheet_test.dart` ended at the first closing brace and left half a group behind; `flutter analyze`
  caught it, and it was repaired by hand, not restored. Staged: the step's files and the two 0074 records. Commit: the
  owner's.
* **Deviation 3, 2026-11-11 — `confirm_required` is live, so D35 cannot remove it.** Evidence: MADR D35 and this step
  remove `confirm_required` and the step's search expects none outside `docs/`; `protocol.ErrConfirmRequired` is sent
  by the codex surface for permanent, section and project deletion (`internal/ws/codex_handlers.go:366`, `:385`, `:404`)
  and by `internal/ws/codex_execution.go:224`, `:228`. Only provider auth's use is dead. Pre-existing: the MADR asserted
  it. Resolutions put to the owner: remove only the provider-auth half and keep the wire code (recommended); remove the
  code and move the codex deletions to another, which changes live behaviour D35 did not consider. Decision: the owner
  chose the first ("option 1", 2026-11-11, in this session). Scope as built above; the MADR's D35 carries the
  narrowing and the search of this step and of 16.5 item 6 leaves `confirm_required` out.
* **Step 6, 2026-11-11 — D37, grok's aggregate status.** After step 5 (`6c8f2623`) and the A9 records (`03d2fdd5`).
  `internal/provider/grok/auth.go`: `authStatus` is configured when `XAI_API_KEY` is set or `configuredMethods`
  reports a method, so a stub `auth.json` is not configured; it used to count the file configured if it merely existed
  (`credstore.FileExists`), while the per-method reader validates it. The two status readers now agree. Tests, in
  `grok/auth_test.go`: `TestGrokStubAuthJSONIsNotConfigured` (a `{}` file: the aggregate, the upstream and every method
  say missing or not configured); `TestAuthStatusSeesAuthJSON` changed, since it wrote `{}` and wanted `configured`,
  the very rule D37 reverses, and now writes the credential `liveGrokCred` (the per-method reader accepts it). Seen
  failing on its planted break in a scratch clone of `03d2fdd5` (`step6_plant.log`), `credstore.FileExists` put back:
  `aggregate status = "configured" for a stub auth.json, want missing`. Passes clean. Gates, on that clone with the
  step's two files: `make pre-add-check FILES=…` "2 file(s) clean"; `make test` and `go test -race -count=1 ./...` 50
  packages ok each, no failure; `make lint`, `make staticcheck`, `make vet-tags` clean. No Dart file changed, and the
  hosts are not run (the conventions name steps 3, 7 and 8). Staged: `grok/auth.go`, `grok/auth_test.go` and this
  record. Commit: the owner's.
* **Step 7, 2026-11-11 — D38, the wiring (Q17 re-asked; Q-D).** Run after step 6 (`589753ad`), on the owner's word to
  proceed. `codex/auth.go`: `SetCredential` keeps its validation and then runs `SetCredentialCoordinated` when
  `p.coord != nil`, else the unexported `setCredentialDirect` (its former body); `ClearCredential` runs
  `CoordinatedLogout` when `p.coord != nil`, else the unexported `runLogout` (its former body). Both fall back to the
  direct body on `errEarlyUnsupportedBackend` and on nothing else. `codex/logout.go`: `errEarlyUnsupportedBackend`
  and `early()`, which wraps beside the original error so `errors.Is` still finds
  `providerauth.ErrUnsupportedBackend`; `SetCredentialCoordinated`'s `CheckBackend` refusal, `CoordinatedLogout`'s
  `MarkRevoked` refusal and, on the non-revoking path, `RecordLogout`'s are marked early; the revoking path's
  `RecordLogout`, which runs after `MarkRevoked`, is not. The revoking path copies LIVE to
  `<tmp>/auth.json` before `MarkRevoked` (`copyLiveForRevoke`: new `os.MkdirTemp` home `0700`, file `0600`, removed by
  a deferred `RemoveAll`; LIVE absent copies nothing and runs no revoke), then `MarkRevoked`, `RecordLogout`, and
  `runLogoutIn(ctx, tmp)`; it no longer calls `ClearCredential`, so the routing cannot recurse. Tests, in
  `codex/phone_paths_test.go`: step 1's `TestPhoneCodexLogoutMarksRevoked` and `TestPhoneCodexKeyWriteCommitsCurrent`
  (failing at `bcde74ab` as step 1 recorded, passing now); `TestCoordinatedLogoutRevokesWithTheToken` (a stub whose
  `logout` fails unless its home holds the refresh token, and journals the home and whether LIVE was already gone);
  `TestCoordinatedLogoutRemovesTheCopy` (the revoke fails; `RecordLogout` fails after the copy, through a lock-path
  fault); `TestClearCredentialDoesNotRecurse`; `TestUnprotectableCodexCredential` (three subtests, below);
  `TestLateUnsupportedBackendIsReturned` (an adapter whose lock path fails with `ErrUnsupportedBackend` once armed, so
  `RecordLogout` refuses after `MarkRevoked` ran). Changed: `TestChatGPTLogoutTombstonesBeforeRevoking` asserted the
  revoke ran "against the real home", which was the defect, and now asserts it ran in a `mcremote-codex-revoke-` copy,
  not the daemon's home and not a verify-clone. Each new test was seen failing on its planted break in a scratch clone
  of `589753ad` (`step7_plants.log`):
  * the routing removed: `state=recovery_required … recovery_available=true generations=1` after a logout, and
    `state=recovery_required, CURRENT holds the new key=false` after a key write;
  * today's order (`runLogout` after `RecordLogout`): `credential removed but revoke did not confirm: codex logout: exit
    status 1`, the stub finding no token;
  * the `RemoveAll` not deferred: `a failed revoke left [… mcremote-codex-revoke-…] behind`, and the same after a failed
    `RecordLogout`;
  * `CoordinatedLogout` calling `ClearCredential` again: `codex logout ran 0 times, want once`;
  * the fallback removed: `the phone's clear on an external host: codex: coordinator refused before changing anything`,
    and the same for the `CheckBackend` key write (a first plant that dropped the last use of `errors` failed the build
    and was redone so it compiled);
  * the fallback taken on any unsupported backend: `ClearCredential = <nil>, want the unsupported-backend error returned`;
  * the backend check removed from the logout (deviation 5): `codex logout runs = [], want one in the daemon's home`.
  All pass clean. Gates, on a scratch clone with the step's four files: `make pre-add-check FILES=…` clean; `make test`
  and `go test -race -count=1 ./...` 50 packages ok each; `make lint`, `make staticcheck`, `make vet-tags` clean. The
  Linux test host, `go test -race -count=1 ./...`: ok, no failure or race. The Windows test host, `make ci-windows`:
  "ALL SELECTED CHECKS PASSED". The first host runs, before deviation 5's change, also passed and are not counted.
  Not done here, by the plan: the live check that OpenAI refuses a refresh with the revoked token, which needs a
  sign-in and is `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1 (MADR
  16.5 item 14). Staged: the step's four files and the two 0074 records. Commit: the owner's.
* **Deviation 4, 2026-11-11 — the plan's `TestUnprotectableCodexCredential` merged two cases that the code and the MADR
  keep apart.** Evidence: the test text expects the phone's key write on a `StateExternal` manifest to run `codex login
  --with-api-key` directly and leave the manifest byte-identical. Built that way it failed: the write went through the
  coordinator, in a pending home, and succeeded (`phone_paths_test.go`, first run). `SetCredentialCoordinated` refuses
  only through `CheckBackend`; `StateExternal` refuses `MarkRevoked` and `RecordLogout`, the logout (D38). MADR §15.13
  holds that an external credential is deliberately not a refusal for a sign-in, so the write can move it under
  protection. Pre-existing: the plan's text. Resolutions put to the owner: assert what the MADR decides, a logout test
  for the external manifest and a separate key-write test (recommended); change the code so an external manifest also
  sends the key write direct, reversing §15.13. Decision: the owner chose the first ("1 and 1", 2026-11-11, in this
  session). Scope: the test is three subtests, an external manifest (the clear), a key write on an external manifest
  committing through the coordinator, and `CheckBackend` refusing a key write; the MADR's D38 carries the refinement.
* **Deviation 5, 2026-11-11 — a phone logout on a keyring host could be tombstoned and do nothing.** Evidence: the
  plan runs no revoke when LIVE is absent. A credential held in a keyring has no `auth.json`, so with the manifest not
  yet external `CoordinatedLogout` would record a logout, remove nothing and revoke nothing, where the direct `codex
  logout` it replaces cleared the keyring credential. Pre-existing in the plan's design. Resolutions put to the owner:
  `CoordinatedLogout` runs `CheckBackend` first and refuses early, so the caller falls back to the direct logout
  (recommended); leave it and record the gap. Decision: the owner chose the first ("1 and 1", 2026-11-11). Added to the
  step's scope: the check in `CoordinatedLogout` and the subtest "a logout where CheckBackend refuses and LIVE is
  absent". A probe of `codex doctor --json` is now part of a phone logout, as it already is of a key write.
* **Step 8, 2026-11-11 — D42, recovery state on the status payload; held at deviation 6.** The coordinator committed
  step 7 as `6bf1e9a4`, on the owner's authorization, after the repository was handed to the coordinator. Run on a
  scratch clone of `6bf1e9a4`. `internal/protocol/messages.go`: `ProviderAuthPayload` gains `BackupState`
  (`json:"backup_state,omitempty"`) and `RecoveryAvailable` (`json:"recovery_available,omitempty"`).
  `internal/ws/server.go`: `authStatePayload` copies both from `AuthState`, so `providers.list_result` and
  `provider.auth_status` carry them. grok: a new `internal/provider/grok/status.go` (the step's option over
  `owned_device_auth.go`) gives `CoordinatedProvider` its own `AuthStatus`, the plain provider's picture plus
  `backupProjection` from `c.coord`, by codex's rule (`backupProjection`, now `codex/logout.go:301`, cited as `:230`
  before step 7 moved it): no coordinator, or a `Status` error, reports nothing. codex's other branch, an
  unprotectable store reported `unsupported`, has no grok counterpart, since grok's credential is always its
  `auth.json`. The phone: `models.dart`'s `ProviderAuthInfo` reads `backup_state` (null when absent or empty) and
  `recovery_available` (absent is false), and a `BackupState` class names the seven states;
  `provider_detail_screen.dart` shows a "Credential backup" row at the top of Credentials when either is sent, with a
  sentence per state, `Backup state: <state>` for one this phone does not know, and, when `recovery_available` is
  true, "A backup can be restored on the host with mcremote auth-recovery." It shows no path, hash or secret (D24).
  Tests: `TestStatusCarriesRecoveryState` (`internal/ws/status_recovery_test.go`): a coordinated codex and a
  coordinated grok, each seeded so its coordinator reports `current` with a restorable CURRENT, carry both members
  over `providers.list` on v2; an uncoordinated kilo stand-in, codex and grok carry neither, and the kilo block is
  byte-identical to the block `6bf1e9a4` sends, captured from that commit in the clone before any step 8 code was
  copied in. `TestGrokStatusProjectsBackup` (`grok/status_test.go`): the wrapper reports its coordinator's state and
  keeps the plain provider's picture; with no coordinator it reports neither. In `provider_detail_screen_test.dart`,
  a group of ten widget tests built from the wire shape through `ProviderInfo.fromJson`: one per backup state, one
  for an unknown state, one for `recovery_available`, and one for an agent with no coordinator. No existing test or
  fixture changed. Each new assertion was seen failing on its planted break in the clone (`step8_plants.log`):
  * the copy removed from `authStatePayload`: `codex's auth block lacks the recovery members (backup_state false,
    recovery_available false)`;
  * `omitempty` dropped from the two tags: `uncoordinated kilo's auth block carries a recovery member`;
  * an uncoordinated block changed with no recovery member (`upstream_switch_unsupported` inverted): `an
    uncoordinated provider's auth block changed`;
  * grok's projection returning empty: `coordinated grok status: backup_state="" recovery_available=false, want
    "current" and true (the coordinator's projection)`;
  * grok with no coordinator projecting `unmanaged`: `grok with no coordinator: backup_state="unmanaged"
    recovery_available=false, want neither`;
  * the wrapper dropping the plain provider's picture: `coordinated grok status lost the plain provider's picture`;
  * `backup_state` not read by the model: `Found 0 widgets with key [<'provider-backup-grok'>]` for each of the
    seven states, and `Found 0 widgets with text "Backup state: quarantined"`;
  * `recovery_available` not read: `Found 0 widgets with key [<'provider-backup-restore-grok'>]`;
  * the row shown with neither member: `Expected: no matching candidates  Actual: … Found 1 widget with key
    [<'provider-backup-grok'>]`.
  All pass clean. Gates, on the clone with the step's eight files: `make pre-add-check FILES=…` "5 file(s) clean
  (gofmt, golint, govulncheck)"; `make test` and `go test -race -count=1 ./...` 50 packages ok each, no failure or
  race; `make lint` clean; `make staticcheck` clean (56 ignored findings, the known acp-go-sdk2 names); `make
  vet-tags` nine tags ok; `flutter analyze` "No issues found"; `dart format --set-exit-if-changed` "231 files (0
  changed)"; `flutter test` "+1542 ~3: All tests passed". The Linux test host, `go test -race -count=1 ./...`: 50
  packages ok, no failure or race. The Windows test host, `make ci-windows`: "ALL SELECTED CHECKS PASSED". Neither
  host kept its scratch copy. ~~**Not staged:** deviation 6 is open, and the step waits on the owner's answer.~~
  *2026-11-12, on the owner's answer to deviation 6 (option 1):* `docs/guides/protocol-v1.md`'s auth-block sentence
  lists `backup_state?` and `recovery_available?`, and a new paragraph after it gives their meaning, the seven
  states, and that an uncoordinated provider and an older daemon omit both. The tests that read the guide pass on the
  tree with it: `go test -count=1 ./internal/protocol ./internal/event ./internal/ws ./internal/provider/httpagent`,
  4 packages ok (`TestEventTypesAreDocumented`, `TestErrorCodesAreDocumented` and
  `TestSessionModeDangerousIsDocumented` among them); none holds the auth block's members, as deviation 6 says, so no
  new assertion is added and none needed a plant. Records: `python3 -B scripts/check_records.py --check-all` exit 0,
  no output; `make markdownlint-docs` "60 files, 0 issues"; the redact preview "would edit 0 file(s)". Staged: the
  step's eight files, the guide and this record. Commit: the coordinator's.
* **Deviation 6, 2026-11-11 — the protocol guide describes the auth block, and step 8 does not list it.** Evidence:
  `docs/guides/protocol-v1.md:1101-1102` reads "An `auth` block (in `providers.list_result` and
  `provider.auth_status`) is `{status, active_upstream?, upstreams[]?, upstream_switch_unsupported?}`". Step 8 adds
  `backup_state` and `recovery_available` to that block, and the step's file list does not name the guide. No test
  holds the block's members against the guide (`internal/protocol/doc_coverage_test.go` checks event types and error
  codes only), so every gate passes with the guide unchanged, unlike step 4's error codes. The gap does not exist at
  `6bf1e9a4`; step 8 makes it. Resolutions put to the owner: (1) add both members to that sentence, with one sentence
  saying what they are and that an uncoordinated provider and an older daemon omit them (MADR D42), in step 8's
  commit, with `docs/guides/protocol-v1.md` added to the step's scope (recommended; one paragraph); (2) leave the
  guide to step 9's close-out, so step 8's commit ships members the wire contract does not yet describe; (3) leave
  it, so the guide stays wrong until the cut-over of
  `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` rewrites the block. Doing
  nothing is (3). ~~Decision: pending.~~ Decision, 2026-11-12: the owner chose option 1, relayed by the coordinator
  session. Added to step 8's scope: `docs/guides/protocol-v1.md`, whose auth-block sentence now lists
  `backup_state?` and `recovery_available?`, followed by a paragraph saying what they are and that an uncoordinated
  provider and an older daemon omit both.
* **Step 9, 2026-11-12 — close-out.** The coordinator committed step 8 as `4bb23579`, on the owner's authorization,
  with deviation 6 decided by the owner (option 1). Start state: `master` at `4bb23579`, two commits ahead of
  `origin/master` (`589753ad`, step 6, whose CI run 43134895855 concluded `success`), the tree clean and the index
  empty. Each of the MADR's 16.5 items 1 to 13 was checked against the tree at `4bb23579`: the code and tests it names
  were read, and the tests it names were run on the tree, `go test -count=1 -v -run '<the tests below>'` over
  `httpagent`, `providerauth`, `grok`, `codex`, `ws`, `protocol` and `provider`: 7 packages ok, 65 tests passed, none
  failed or skipped (`items_go.log`). "Seen failing" is each step's own plant above, not run again here.

  | Item | State | Commit | Evidence |
  | :--- | :--- | :--- | :--- |
  | 1 no label decides a flow | met | `14200b6e` (step 2) | `TestNoLabelDecidesTheFlow`: "Login with a browser" is a startable `oauth_device` with id `vendor:login-with-a-browser`, and its loopback authorize URL is refused at start with `ErrAuthUnsupported`, "signs in through a browser on the host"; seen failing with the marker list restored (step 2). `git grep` finds no `browserOAuthMarkers`, `oauthType` or `EngineMethodIndex` outside `docs/`; `ClassifyCatalogMethod(typ)` reads the type alone |
  | 2 a reordered kilo catalog | met | `14200b6e` (step 2) | `TestKiloMethodIDsSurviveAReorder`: the method listed second is started at index 0 after the reorder; seen failing with index ids (`listed id = "xai:1"`, step 2). `TestAmbiguousSlugIsRefused` and `TestIndexMethodIDsAreRefused` hold the refusals |
  | 3 grok's code lifetime and the deadline | met | `f6e99522` (step 3) | `TestOwnedGrokFlowReportsItsCodesLifetime` (`expires_in` = `grokDeviceExpirySeconds`, 600, `grok/device_auth.go:22`); `TestOwnedFlowExpiresAtItsDeadline` (ends `expired`, retryable, reaped); `TestReadyToActivateOutlivesTheCode`; `TestDeadlineSurvivesDetachAndResume` (detach, resume, then `expired`); each seen failing (step 3). The ActivationGrace end is held by the owned flow's own timer, started at `ready_to_activate` and defaulting to `ActivationGrace` (`providerauth/ownedflow.go:16`, `:109-110`, `:232-244`), which the pre-existing `TestOwnedFlowActivationDeadlineAborts` (P20) holds with an injected 150 ms; the reservation's re-armed deadline (`reservation.go:204-212`, `armGrace`) is a backstop that was read, not run: no test drives the five-minute constant, and no plant of P23 broke it |
  | 4 `deviceAuthWired` | met | `f6e99522` (step 3) | `TestDeviceAuthWiredForOwnedFlows`: an owned flow with no legacy hook, and one whose legacy hook says no, are wired; seen failing on the old rule (step 3). `server.go:2363-2372` asks `provider.OwnedDeviceAuth` first |
  | 5 D36's codes | met | `ea18ceff` (step 4) | `TestCoordinatorSentinelsHaveTheirOwnCodes`: the five sentinels, bare and wrapped, map to `provider_auth_busy`, `credential_conflict`, `unsupported_backend`, `recovery_required` and `too_many_sign_ins`, never `credential_failed` (`server.go:2822-2838`); seen failing with the cases removed (step 4); the phone's copy for each is held by `friendly_op_error_test.dart` (step 4) |
  | 6 D35's names gone | met, as narrowed by deviation 3 | `6c8f2623` (step 5) | `git grep -n 'keyring_managed\|AuthQuota\|AuthStatusQuota\|ZeroString\|CancelDevice\|ErrAuthConfirmRequired\|confirmDestructiveSignIn\|"host_oauth"' -- ':!docs'` finds one line, the one definition `internal/provider/auth.go:33`, `const AuthReasonHostOAuth = "host_oauth"`, which D35 asks for (`item6_grep.log`). `confirm_required` is left out by deviation 3; codex's legacy `StartDeviceAuth` is gone (`codex/device_auth.go:8` is a comment saying so) |
  | 7 the phone's codex dialog and the result sheet | met | `6c8f2623` (step 5), `ea18ceff` (step 4) | No `confirmDestructiveSignIn`, no destructive sign-in dialog and no "signs the host out of ChatGPT" under `apps/mobile/lib`; `providerId == 'codex'` is the removal copy alone (`provider_detail_screen.dart:724`, `:702` at step 5, moved by step 8's row). `device_flow_sheet_test.dart` renders each terminal state: its group "renders each result state" covers `cancelled`, `expired`, `conflict`, `recovery_required` and `unsupported_backend`, with "completed shows no retry hint", "reports failure with the reason" (`failed`) and the older-daemon fallback beside it; seen failing with `ok` read alone (step 4); run in the full `flutter test` of item 8 |
  | 8 the gates | met | `4bb23579` | `make preflight` on a scratch clone of `4bb23579`, after `flutter pub get`: exit 0, "preflight passed": check-records clean; markdownlint 60 files, 0 issues; gofmt and `go mod tidy` clean; `go vet` and `vet-tags`; staticcheck for linux, darwin and windows (56 ignored findings each, the known acp-go-sdk2 names); `go test -race ./...` 50 packages ok; the install tests ok; systemd units skipped (no `systemd-analyze` on this host); the release build; the Flutter pin; pub advisories none; `dart format` "231 files (0 changed)"; `flutter analyze` "No issues found"; `flutter test` "+1542 ~3: All tests passed" (`preflight.log`). `make race` on the clone: exit 0, 50 packages ok, no race (`race.log`). The Linux test host's `go test -race -count=1 ./...` (50 packages ok, no failure or race) and the Windows test host's `make ci-windows` ("ALL SELECTED CHECKS PASSED") ran at step 8 on its eight code files, which are `4bb23579`'s; the guide paragraph of deviation 6 came after them and changes no code, and the tests that read the guide pass in this step's runs. Neither host was run again here. CI has run on `589753ad` only (43134895855, `success`); steps 7 and 8 are not pushed, so no CI run holds them yet |
  | 9 the phone's logout and key write leave the coordinator right | met | `6bf1e9a4` (step 7) | `TestPhoneCodexLogoutMarksRevoked` (after `ClearCredentialMethod` and `Recover`: `logged_out`, `BackupState` `logged_out`, `RecoveryAvailable` false, no generation) and `TestPhoneCodexKeyWriteCommitsCurrent` (CURRENT holds the key, the start sets no `recovery_required`); both failing at `bcde74ab` on the direct paths (step 1) and with the routing removed (step 7) |
  | 10 no coordinator, no recursion | met | `6bf1e9a4` (step 7) | `TestCoordinatedLogoutNeedsACoordinator` and the codex `TestSetCredential*` tests, which build no coordinator, pass; step 7 changed no test of the package but `TestChatGPTLogoutTombstonesBeforeRevoking`, which builds one (`git diff 589753ad 6bf1e9a4 -- internal/provider/codex`: `auth.go`, `logout.go`, `logout_test.go` and the new `phone_paths_test.go`). `TestClearCredentialDoesNotRecurse`: one `codex logout`, inside the test's 30 s bound; seen failing with `CoordinatedLogout` calling `ClearCredential` again (step 7). Step 5 deleted `codex/device_auth_test.go` with the legacy flow it tested (D35), before D38 |
  | 11 recovery state on the status payload | met | `4bb23579` (step 8) | `TestStatusCarriesRecoveryState`: coordinated codex and grok carry `backup_state` `"current"` and `recovery_available` `true`; uncoordinated kilo, codex and grok carry neither, and kilo's block is byte-identical to `6bf1e9a4`'s; `TestGrokStatusProjectsBackup`; `provider_detail_screen_test.dart`'s group "credential backup (MADR 0074 D42)"; each seen failing with the copy removed and the other plants of step 8 |
  | 12 the revoke runs with the token | met | `6bf1e9a4` (step 7) | `TestCoordinatedLogoutRevokesWithTheToken`: the stub's `logout` exits 1 unless its home holds the refresh token; one run, `live_gone` (after the tombstone), in a `mcremote-codex-revoke-` copy and not the daemon's home, the copy gone afterwards, `logged_out` and no recovery; seen failing with today's order planted (`revoke did not confirm`, step 7). `TestCoordinatedLogoutRemovesTheCopy` holds the removal on a failed revoke and a failed `RecordLogout` |
  | 13 an unprotectable credential | met, as refined by deviation 4 | `6bf1e9a4` (step 7) | `TestUnprotectableCodexCredential`: on a `StateExternal` manifest the clear is one direct `codex logout` in the daemon's home and the manifest is byte-identical; where `CheckBackend` refuses, the key write is one direct `login --with-api-key` and the logout (LIVE absent, deviation 5) one direct `codex logout`, each manifest byte-identical. A key write on a `StateExternal` manifest commits through the coordinator, as deviation 4 decided by §15.13, so item 13's "and key write" on that manifest is not the built behaviour, by the owner's decision. `TestLateUnsupportedBackendIsReturned`: a refusal after `MarkRevoked` is returned, unmarked as early, with no fallback logout. Each seen failing (step 7) |

  Item 14 is open: the owner-run check that OpenAI refuses a refresh with the token a phone logout revoked needs a
  sign-in and belongs to `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1,
  as step 7 says. It is not run here.

  **The protocol guide's `confirm_required` sentence (flagged after step 5).** `docs/guides/protocol-v1.md:1139-1142`
  reads "`confirm_required` — a destructive operation on the codex surface (permanent, section or project deletion)
  was sent without the client's confirmation; provider auth never sends it". The deletion half and "provider auth
  never sends it" are correct at `4bb23579`: `internal/ws/codex_handlers.go:366`, `:385` and `:404` send it for
  permanent, section and project deletion, and no provider-auth path sends it (item 6's search, deviation 3). The
  sentence is incomplete: `internal/ws/codex_execution.go:224` and `:228` send the same code for codex execution
  writes, an unsandboxed `shell` or `spawn` without its fresh confirmation phrase and a `select_environment` without
  its confirmation, which are not deletions. The same gap is in `ErrConfirmRequired`'s comment
  (`internal/protocol/errors.go:184-188`) and in the MADR's D35 note of deviation 3, which cites
  `codex_execution.go` as a deletion. No test reads the sentence (`TestErrorCodesAreDocumented` asks only that the code
  appear). Nothing is changed here: the guide and the comment are not this step's files, and the fix waits for the
  owner.

  **Found while checking, outside items 1 to 13, not changed.** The phone's `startProviderDeviceAuth` keeps a
  `confirmDestructive` parameter (`apps/mobile/lib/data/ws/mcremote_client.dart:3720-3737`) that no caller sets since
  step 5, documented as "required for … codex's, which signs the host out the moment it starts (D8). Without it the
  daemon refuses"; `protocol.StartAuthPayload.ConfirmDestructive` (`internal/protocol/messages.go:914-923`) says the
  same, and no daemon code reads the field. D32 and D35 do not name either, so neither is a miss of this phase; both
  comments are stale since the legacy codex flow went.

  Statuses: this phase `complete`, with item 14 open under
  `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7; this PLAN's status bullet
  and top note say so, and §17.6's P22 row is annotated as moved (D40), as §23 steps 5 and 6 were at step 0. The
  MADR's status cell, §16's status and 16.5 carry dated notes, 16.5's naming deviations 3 and 4 for items 6 and 13.
  The whole PLAN's status is not changed: step 9 sets this phase's. The index lists no status, so it is unchanged.
  Checks: `python3 -B scripts/check_records.py --check-all` exit 0, no output; `make markdownlint-docs` "60 files,
  0 issues"; the redact preview, `python3 ~/.global-git-hooks/github-disclosure.py redact`, "would edit 0
  file(s)"; the item tests and gates above (`items_go.log`, `item6_grep.log`, `preflight.log`, `race.log`).
  Staged: the two 0074 records. Commit: the coordinator's.

## 26. Phase P24 — decided 2026-11-12: a cleanup after P23 (`confirm_required`'s text and the dead `confirmDestructive` names)

**Status: ~~proposed 2026-11-12, held on Q24-A~~ ~~approved 2026-11-12, not yet executed~~ complete 2026-11-12
(the execution record below).** The owner decided this
step on 2026-11-12, in the coordinator session. Drafting found that one fact the decision rests on differs from the
code (below, "Where the code differs from the decision"), and ~~the step does not run until the owner answers
Q24-A~~ the owner answered Q24-A (a) and approved the step on 2026-11-12, in the coordinator session. It runs after
`0179-PLAN-gobble-native-acp-provider.md` P7c, which landed as `4614263c`, and before the release. It is one commit.

### Goal

P23 step 9 found two leftovers of the legacy codex device flow. `confirm_required` is described in three places as
a deletion code, and the codex surface also sends it for execution writes. The field and parameter the phone once set
to confirm a destructive codex sign-in remain, with nothing left to confirm. After P24 the three descriptions say what
the code sends, both names are gone, and this PLAN is complete.

### The owner's decision (2026-11-12)

> "Small 0074 cleanup step" — amend 0074's PLAN with a dated step P24 that (1) corrects `docs/guides/protocol-v1.md`'s
> `confirm_required` text, the `ErrConfirmRequired` comment in `internal/protocol/errors.go` (~:184-188) and the
> 0074-MADR D35 note from deviation 3, so they say `confirm_required` is the codex surface's code for actions that need
> an explicit confirmation — the permanent, section and project deletions (`internal/ws/codex_handlers.go`
> ~:366/385/404) AND an unsandboxed `shell`/`spawn` without its confirmation phrase and `select_environment` without
> confirmation (`codex_execution.go` ~:224/228) — and that provider auth never sends it; and (2) removes the two dead
> names the legacy codex device flow left: `protocol.StartAuthPayload.ConfirmDestructive`
> (`internal/protocol/messages.go` ~:914-923, read by no daemon code) and the phone's unused `confirmDestructive`
> parameter on `startProviderDeviceAuth` (`apps/mobile/lib/data/ws/mcremote_client.dart` ~:3720-3737), with tests.

The owner also decided that this PLAN is marked complete when P24 lands, with W3 (the loopback tunnel) recorded as
deferred to a record of its own and the MADR's 16.5 item 14 as owned by
`0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1.

### What the code says (re-read at `4614263c`)

* **`confirm_required`'s senders.** `codex.threads.write` sends it for a `delete` whose `confirm` is not "delete
  permanently", a `delete_section` without "delete section" and a `delete_project` without "delete project"
  (`internal/ws/codex_handlers.go:366`, `:385`, `:404`). `codex.execution.write` sends it for a `shell` or `spawn`
  whose `confirm` is not `confirmUnsandboxed` ("run unsandboxed") and for a `select_environment` without
  `confirmEnvironment` ("change execution environment") (`internal/ws/codex_execution.go:223-230`, the phrases at
  `:36`, `:38`). No provider-auth path sends it (P23 deviation 3; the MADR's 16.5 item 6).
* **The three texts.** `docs/guides/protocol-v1.md:1139-1142` names only the deletions. `ErrConfirmRequired`'s comment
  (`internal/protocol/errors.go:184-187`) says "the operation is destructive" and names only the deletions. The
  MADR's D35 note of deviation 3 calls the code "the codex surface's live code for a deletion sent without
  confirmation" and cites `codex_execution.go` as one. None of the three mentions the execution writes.
* **`StartAuthPayload.ConfirmDestructive`** (`internal/protocol/messages.go:912-924`). Its comment says the daemon
  refuses a destructive flow without it. No flow is destructive, and nothing refuses.
* **The phone's `confirmDestructive`** (`apps/mobile/lib/data/ws/mcremote_client.dart:3720-3737`). Its comment says
  "Without it the daemon refuses". The only caller (`apps/mobile/lib/features/settings/provider_detail_screen.dart:609`)
  has not passed it since P23 step 5, and no test calls `startProviderDeviceAuth`.
* **The guide's `provider.start_auth` row** (`docs/guides/protocol-v1.md:1095`) lists `confirm_destructive?`. The
  decision does not name this row. It is in the same file as (1) and becomes false with (2).
* **Wire compatibility.** The guide says "future protocol fields are optional and must be ignored when unknown"
  (`docs/guides/protocol-v1.md:1942`). `handleStartAuth` decodes with `json.Unmarshal` (`internal/ws/server.go:2639`),
  which ignores unknown members. A phone built before P23 step 5 sends `confirm_destructive: true` for codex. ~~The
  daemon must go on accepting that frame, so P24 adds a test that holds it (step 4).~~ *Dropped 2026-11-12 (deviation 2).*

### Where the code differs from the decision

**The daemon does read the field.** `handleStartAuth` passes `p.ConfirmDestructive` to the legacy
`provider.DeviceAuth.StartDeviceAuth` (`internal/ws/server.go:2665`). That method's `confirmDestructive bool`
parameter (`internal/provider/auth.go:281-284`) runs through `acpagent.Spec.StartDeviceAuth`
(`internal/provider/acpagent/acpagent.go:157`, `:422-426`) and `httpagent.DeviceAuthDialect`
(`internal/provider/httpagent/httpagent.go:296`, `:307-315`) to four implementations. Each discards it as `_ bool`:
grok (`internal/provider/grok/device_auth.go:59`), gobble (`internal/provider/gobble/auth.go:359`), opencode
(`internal/provider/opencode/acp_auth.go:621`) and kilo (`internal/provider/kilo/device_auth.go:21`).
`auth.go:281-283` says so itself: "so no implementation reads it". So the field is read and passed on, and nothing
acts on it. Removing the field breaks the build at `server.go:2665`, a file the decision does not name.
`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D11 removes `provider.start_auth`,
its payload and `DeviceAuth` at its cut-over.

### Q24-A, ~~open~~ answered (a) 2026-11-12: how far the removal goes

* **(a) Recommended: remove the whole chain.** Remove the field, `server.go:2665`'s argument, and the parameter from
  `provider.DeviceAuth`, `acpagent.Spec.StartDeviceAuth`, `acpagent.Provider.StartDeviceAuth`,
  `httpagent.DeviceAuthDialect`, `httpagent.Provider.StartDeviceAuth` and the four implementations, together with
  their test call sites. Cost: ten Go production files and eight test files, each change a dropped `bool` argument or
  parameter or a comment, in code that 0246's cut-over rewrites anyway. Result: the name is gone from the daemon and
  the phone, which is what the decision intends.
* **(b) Remove the field and pass `false` at `server.go:2665`.** The parameter stays on `provider.DeviceAuth` and
  below, read by nothing, until 0246 removes the interface. Its comment (`auth.go:281-283`) is corrected to say it
  carries nothing. Cost: `server.go` and `auth.go` are added to the decision's files. The daemon passes a constant
  that every implementation discards, and step 5's search finds the parameter chain.
* **(c) Keep the field. Remove only the phone's parameter, and correct the field's comment.** Cost: the least. The
  dead wire field stays until the cut-over, which goes against the decision's (2).

**Answer, 2026-11-12:** the owner chose (a), in the coordinator session, relayed by the coordinator.

Doing nothing means the decision's (2) cannot run as written, because removing the field alone does not build. The
steps below are written for (a). Under (b), step 2's chain shrinks to `server.go:2665` and `auth.go`'s comment,
`TestStartDeviceAuthPassesTheMethodAndInputs` keeps its `confirm` assertion, and step 5's search expects the chain.
Under (c), step 2 is the field's comment alone, the protocol test of step 4 is not written, and step 5's search
expects the field.

### Files

* Records and guide: this PLAN; `docs/decisions/0074-MADR-remote-provider-auth-from-phone.md`;
  `docs/guides/protocol-v1.md`.
* Go, production: `internal/protocol/errors.go` (comment), `internal/protocol/messages.go`, `internal/ws/server.go`,
  `internal/provider/auth.go`, `internal/provider/acpagent/acpagent.go`, `internal/provider/httpagent/httpagent.go`,
  `internal/provider/grok/device_auth.go`, `internal/provider/gobble/auth.go`, `internal/provider/opencode/acp_auth.go`,
  `internal/provider/kilo/device_auth.go`.
* Go, tests changed: `internal/provider/acpagent/credential_test.go`, `internal/provider/gobble/auth_test.go`,
  `internal/provider/grok/live_auth_test.go` (`live_grok`), `internal/provider/kilo/device_auth_test.go`,
  `internal/provider/kilo/live_auth_write_test.go` (`live_kilo`), `internal/provider/opencode/acp_auth_test.go`,
  `internal/ws/device_auth_wired_test.go`, `internal/provider/codex/owned_device_auth_test.go` (two comments).
* Go, tests added: `internal/protocol/provider_auth_test.go` (one test)~~, `internal/ws/provider_start_auth_test.go`
  (new)~~. *Dropped 2026-11-12 (deviation 2).*
* Dart: `apps/mobile/lib/data/ws/mcremote_client.dart`; `apps/mobile/test/provider_start_auth_client_test.dart` (new).

No fixture or golden changes. If a gate shows one changing, the step stops with a dated entry.

### Steps

1. **The three texts (decision (1)).**
   * `docs/guides/protocol-v1.md:1139-1142`: `confirm_required` is the codex surface's code for an action sent
     without the explicit confirmation it needs. That is a `codex.threads.write` permanent, section or project
     deletion without its `confirm` phrase, an unsandboxed `codex.execution.write` `shell` or `spawn` without
     `confirm: "run unsandboxed"`, or a `select_environment` without `confirm: "change execution environment"`.
     Provider auth never sends it (MADR 0074 D35).
   * `internal/protocol/errors.go:184-187`: the same, as a comment. The last sentence (the code once also meant codex
     device auth) stays.
   * The MADR's D35 note: "a deletion sent without confirmation" struck through, followed by a dated italic note
     (2026-11-12, P24). The note names both surfaces and their lines, and says that `codex_execution.go`'s two sends
     are not deletions. Nothing else in the note is rewritten.
2. **The wire field and its chain (decision (2), under Q24-A (a)).**
   * `protocol.StartAuthPayload.ConfirmDestructive` and its comment paragraph removed. The type's comment keeps its
     first line.
   * The guide's `provider.start_auth` row (`docs/guides/protocol-v1.md:1095`) loses `confirm_destructive?`.
   * `internal/ws/server.go:2665` calls `StartDeviceAuth(flowCtx, p.UpstreamID, p.MethodID, p.Inputs)`.
   * The `confirmDestructive bool` parameter removed from `provider.DeviceAuth.StartDeviceAuth`, together with its
     comment (`auth.go:281-283`). `OwnedDeviceAuth`'s comment (`auth.go:337-339`) loses its contrast with a parameter
     that no longer exists: an owned flow runs against an isolated, empty pending home, so there is nothing to confirm.
   * The parameter is removed from `acpagent.Spec.StartDeviceAuth`, `acpagent.Provider.StartDeviceAuth`,
     `httpagent.DeviceAuthDialect`, `httpagent.Provider.StartDeviceAuth`, grok's `startDeviceAuth`, gobble's and
     opencode's `startDeviceAuth`, and kilo's `httpDialect.StartDeviceAuth`.
   * Test call sites drop the argument, and `legacyOff` drops the `bool`.
     `TestStartDeviceAuthPassesTheMethodAndInputs` (`acpagent/credential_test.go:64`) loses `gotConfirm` and its
     `!gotConfirm` clause, because the value it checked is no longer passed. It still holds the bin, upstream, method
     and inputs. `codex/owned_device_auth_test.go:151-158`'s two comments say "takes no confirmation" in place of the
     parameter's name.
3. **The phone's parameter (decision (2)).** `startProviderDeviceAuth` loses `confirmDestructive`, its doc paragraph
   and `if (confirmDestructive) 'confirm_destructive': true`. The caller is unchanged.
4. **Tests, each seen failing on a planted break in a scratch clone, never in the tree.** The record quotes each
   failure line.
   * `TestStartAuthPayloadHasNoConfirmDestructive` (`internal/protocol/provider_auth_test.go`): the JSON member names
     of `StartAuthPayload`'s exported fields, read from their tags, are exactly `provider_id`, `upstream_id`,
     `method_id` and `inputs`. **Planted:** the field restored in `messages.go`.
   * ~~`TestStartAuthIgnoresAnOlderPhonesConfirmDestructive` (`internal/ws/provider_start_auth_test.go`, `ws_test`, on
     `startAuthServer` of `provider_auth_test.go`). A provider with a legacy `provider.DeviceAuth` records its start.
     Over protocol 2, `provider.start_auth` is sent with `confirm_destructive: true` beside the method id. The reply is
     `ok`, an `oauth.device_flow` carries the provider's code, and the provider saw the method id. This is the
     tolerance the guide's line 1942 requires of a removed member. **Planted:** `handleStartAuth` decoding with a
     `json.Decoder` that has `DisallowUnknownFields`, so the reply is `bad_payload`.~~ *Dropped 2026-11-12 (deviation 2).*
   * The chain's removal is compile-checked. **Planted:** one call in `kilo/device_auth_test.go` restored with its
     fifth `false` argument; `go vet ./internal/provider/kilo/` fails with "too many arguments".
   * `apps/mobile/test/provider_start_auth_client_test.dart`, on a stub daemon in the form of
     `projects_list_client_test.dart`'s `_StubDaemon`. `startProviderDeviceAuth` sends `provider.start_auth` whose
     payload keys are exactly `provider_id`, `upstream_id`, `method_id` and `inputs`, with `inputs` absent when
     empty, and it returns on the `ok` reply. **Planted:** `'confirm_destructive': true` added back to the client's
     payload.
   * The parameter's removal is compile-checked on the phone. **Planted:** the caller at
     `provider_detail_screen.dart:609` passing `confirmDestructive: true`; `flutter analyze` reports
     `undefined_named_parameter`.
   * All pass clean in the tree's copy of the step.
5. **Assertions by search, on the final tree.**
   `git grep -n 'confirm_destructive\|confirmDestructive\|ConfirmDestructive' -- ':!docs'` finds only step 4's ~~two Go
   tests: the frame literal of the tolerance test and~~ Go test (deviation 2): the protocol test's member name. ~~`git grep -n confirm_required
   -- ':!docs'` finds the constant, its registry entry and the five senders, as at `4614263c`.~~ *Read 2026-11-12 by
   the owner's decision on deviation 1:* `git grep -n 'ErrConfirmRequired\|confirm_required' -- ':!docs'` finds the
   constant, its registry entry and the five senders, as at `4614263c`.
6. **Gates** run on a scratch clone of the commit that holds this section, with the step's files copied in, as P23's
   steps ran them:
   * `make pre-add-check FILES="<the step's Go files>"`, `make test`, `go test -race -count=1 ./...`, `make lint`,
     `make vet-tags` and `make vulncheck`;
   * `make staticcheck`, with its ignored findings compared by name against the same run at `HEAD`;
   * after `flutter pub get` in the clone: `dart format --output=none --set-exit-if-changed .`, `flutter analyze` and
     `flutter test`;
   * the Linux test host runs `go test -race -count=1 ./...` and the Windows test host `make ci-windows`, through the
     scratchpad's host runner with a fresh tag. Host names are arguments only and are never written to a record;
   * the records: `python3 -B scripts/check_records.py --check-all`, `make markdownlint-docs`, and the redact preview
     `python3 ~/.global-git-hooks/github-disclosure.py redact` (no `--apply`).

   No sign-in, no live run, no quota. The live-tagged tests are compiled by `make vet-tags`, not run.
7. **Close-out.** An execution entry below whose first line is "The coordinator committed P24's plan as `<sha>`, on
   the owner's authorization."
   * This section's status: complete.
   * This PLAN's status bullet and top note: the PLAN is `complete`. W3 (the loopback tunnel) is deferred to a record
     of its own, as the MADR's status cell and §16 (P16) already say, and no number is allocated here. The MADR's 16.5
     item 14 is owned by `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P7's L1.
   * The MADR's status cell gets a dated italic note saying the same.
   * Q24-A's answer is recorded with its date.
   * **Stage** exactly the files above, and show `git diff --cached --stat`.

### Verification

```bash
make pre-add-check FILES="<the step's Go files>"
make test
go test -race -count=1 ./...
make lint staticcheck vet-tags vulncheck
cd apps/mobile && flutter pub get && dart format --output=none --set-exit-if-changed . && flutter analyze && flutter test
go test -race -count=1 ./...        # the Linux test host
make ci-windows                     # the Windows test host
python3 -B scripts/check_records.py --check-all
make markdownlint-docs
python3 ~/.global-git-hooks/github-disclosure.py redact
```

### Rollout and rollback

* The wire changes only by a phone→daemon member that is no longer sent. An older phone that still sends it is
  accepted ~~(step 4's tolerance test)~~ (`json.Unmarshal` ignores the member; untested, deviation 2), and an older
  daemon never acted on it, so a newer phone against an older daemon loses nothing.
* Rollback: revert the commit. No state is written.

### Execution record

* **P24, 2026-11-12 — executed under Q24-A (a).** The coordinator committed P24's plan as `aff9c7b4`, on the owner's
  authorization. Start state: `master` at `aff9c7b4`, seven commits ahead of `origin/master`, clean tree, empty index.
  * **Step 1, the three texts.**
    * `docs/guides/protocol-v1.md`: the `confirm_required` sentence now names `codex.threads.write`'s deletions, the
      unsandboxed `codex.execution.write` `shell` and `spawn` without "run unsandboxed", and `select_environment`
      without "change execution environment", and still says that provider auth never sends it.
    * `internal/protocol/errors.go`: `ErrConfirmRequired`'s comment says the same, and keeps its sentence about the
      legacy codex flow.
    * The MADR's D35 note: "a deletion sent without confirmation" is struck through, followed by a dated italic
      correction (2026-11-12, PLAN P24) that names both surfaces and their lines.
  * **Step 2, the wire field and its chain (Q24-A (a)).**
    * `StartAuthPayload.ConfirmDestructive` and its comment paragraph are gone. `server.go:2665` passes four
      arguments.
    * The parameter is removed from `provider.DeviceAuth`, `acpagent.Spec.StartDeviceAuth`,
      `acpagent.Provider.StartDeviceAuth`, `httpagent.DeviceAuthDialect` and `httpagent.Provider.StartDeviceAuth`,
      and from grok's, gobble's, opencode's and kilo's implementations. `auth.go`'s `DeviceAuth` comment loses its
      paragraph on the parameter, and `OwnedDeviceAuth`'s loses its contrast with it.
    * The guide's `provider.start_auth` row loses `confirm_destructive?`.
    * Test call sites drop the trailing argument (gobble 4, kilo 8 with its live test, opencode 6, grok's live
      test 1, acpagent 1), and `legacyOff` drops its `bool`.
    * Changed test, named with why: `TestStartDeviceAuthPassesTheMethodAndInputs` loses `gotConfirm` and its
      `!gotConfirm` clause, because the value it checked is no longer passed. It still holds the bin, upstream,
      method and inputs.
    * `codex/owned_device_auth_test.go`'s two comments say "takes no confirmation".
  * **Step 3, the phone.** `startProviderDeviceAuth` loses `confirmDestructive`, its doc paragraph and its payload
    line. The caller is unchanged.
  * **Step 4, tests.** The new tests are:
    * `TestStartAuthPayloadHasNoConfirmDestructive` (`internal/protocol/provider_auth_test.go`);
    * ~~`TestStartAuthIgnoresAnOlderPhonesConfirmDestructive` (`internal/ws/provider_start_auth_test.go`);~~ written,
      seen failing on its plant (below), then removed from the tree by deviation 2;
    * `apps/mobile/test/provider_start_auth_client_test.dart`, which holds two tests: one with no inputs (the keys
      are exactly the three method members) and one with inputs (the four).

    Each was seen failing on its planted break in a scratch clone of `aff9c7b4` with the step's 24 files copied in,
    and each plant was restored byte for byte afterwards (`s2_plants.log`):
    * the field restored in `messages.go`: `provider_auth_test.go:169: StartAuthPayload's json members =
      [provider_id upstream_id method_id inputs confirm_destructive], want [provider_id upstream_id method_id
      inputs]`;
    * ~~`handleStartAuth` decoding with `DisallowUnknownFields`: `provider_start_auth_test.go:56: an older phone's
      start_auth was refused: error {"message":"bad payload","code":"bad_payload"}`;~~ (deviation 2: the test is
      dropped);
    * a fifth `false` argument restored in `kilo/device_auth_test.go`: `vet: internal/provider/kilo/device_auth_test.go:96:96:
      too many arguments in call to d.StartDeviceAuth`;
    * `'confirm_destructive': true` added back to the client's payload: `Expected: Set:['provider_id', 'upstream_id',
      'method_id']  Actual: Set:['provider_id', 'upstream_id', 'method_id', 'confirm_destructive']  Which: larger
      than expected`, and the same with `inputs` in the second test;
    * the caller passing `confirmDestructive: true`: `error • The named parameter 'confirmDestructive' isn't defined.
      … • lib/features/settings/provider_detail_screen.dart:614:9 • undefined_named_parameter`.

    All pass clean: ~~the two Go tests~~ the Go test in the tree, and the Dart file "+2: All tests passed!".
  * **Step 5, searches, on the final tree.**
    * `git grep -n 'confirm_destructive\|confirmDestructive\|ConfirmDestructive' -- ':!docs'` (the new files
      included) finds only ~~step 4's two Go tests:~~ `internal/protocol/provider_auth_test.go` (its name, doc comment and
      the member it names)~~ and `internal/ws/provider_start_auth_test.go` (its name, doc comment and the frame literal
      `"confirm_destructive": true`)~~, after deviation 2. Nothing in the Dart sources or tests matches.
    * `git grep -n confirm_required -- ':!docs'` finds one line, the constant (`internal/protocol/errors.go:192`), as
      it does at `4614263c` (`:188`). The step's text expected the constant, the registry entry and the five senders.
      See deviation 1, decided (1): the wider search below is the one step 5 means.
    * `git grep -n 'ErrConfirmRequired\|confirm_required' -- ':!docs'` finds the comment, the constant, the registry
      entry and the five senders in the tree, the same set as at `4614263c` (`codex_execution.go:224`, `:228`;
      `codex_handlers.go:366`, `:385`, `:404`); only `errors.go`'s line numbers moved, by the longer comment.
  * **Step 6, gates**, on the scratch clone with the step's 24 files:
    * `make pre-add-check FILES=…` (the 20 Go files): "20 file(s) clean (gofmt, golint, govulncheck)".
    * `make test`: 50 packages ok. `go test -race -count=1 ./...`: 50 packages ok, no FAIL, no race.
    * `make lint`: clean. `make vet-tags`: live_gobble, live_grok, live_kilo and live_opencode ok, among the rest.
    * `make vulncheck`: "clear; allowlisted required-module findings: GO-2026-5932".
    * `make staticcheck`: no finding. Its output names no ignored finding, only counts ("56 ignored finding(s), each
      an acp-go-sdk2 unstable-schema name" for each of linux, darwin and windows), and it is identical line for line
      to the same run on a clean clone of `aff9c7b4`.
    * After `flutter pub get`: `dart format --output=none --set-exit-if-changed .` "232 files (0 changed)";
      `flutter analyze` "No issues found!"; `flutter test` "+1544 ~3: All tests passed!".
    * The Linux test host, `go test -race -count=1 ./...`: 50 packages ok, no FAIL, no race. The Windows test host,
      `make ci-windows`: "ci-windows-local: ALL SELECTED CHECKS PASSED". Both ran through the scratchpad's host runner
      on a clone refreshed to `aff9c7b4` with the step's files, with a fresh tag, and neither host kept its copy.
    * No fixture or golden changed.
    * Records, on the tree: `python3 -B scripts/check_records.py --check-all` exit 0, no output; `make
      markdownlint-docs` "60 files, 0 issues"; the redact preview "would edit 0 file(s)".
  * **Step 7, close-out.**
    * This section's status is complete.
    * This PLAN's status bullet and top note say the PLAN is `complete`. W3 is deferred to a record of its own, and
      the MADR's 16.5 item 14 is owned by `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`
      P7's L1.
    * The MADR's status cell carries the dated note.
    * Q24-A's answer, (a), is recorded above with its date.
    * ~~Staged: the step's files.~~ ~~**Not staged:** deviation 1 is open, and the step waits on the owner's answer.~~
      Staged after the owner decided deviations 1 and 2 (2026-11-12): see the re-run entry below. Commit: the
      coordinator's.
* **Deviation 1, 2026-11-12: step 5's `confirm_required` search cannot find what the step says it finds.**
  * Evidence: step 5 expects `git grep -n confirm_required -- ':!docs'` to find "the constant, its registry entry and
    the five senders, as at `4614263c`". The literal string occurs once outside `docs/`, in the constant's line. The
    registry entry and the five senders use the identifier `ErrConfirmRequired`. At `4614263c` the same search finds
    only `internal/protocol/errors.go:188`, so the expectation was wrong when it was written.
  * Pre-existing: the plan's text. Nothing P24 changed causes it, and no code or test is affected.
  * Resolutions:
    * (1) Recommended: read the search as it was meant, with `'ErrConfirmRequired\|confirm_required'`, which finds the
      constant, the registry entry and the five senders, the same set as at `4614263c` (shown above). Strike step 5's
      expectation, annotate it with this pattern, and stage. Cost: one annotated line.
    * (2) Keep the literal search and change its expected result to the one constant line. Cost: the same, but the
      search then no longer shows that the senders are unchanged.
  * Doing nothing leaves step 5's text false against the tree. The statuses in step 7's entry assume (1); another
    choice amends them.
  * ~~Decision: pending.~~ Decision, 2026-11-12: the owner chose (1), relayed by the coordinator. Step 5's
    expectation is struck and annotated with the wider pattern; the statuses of step 7's entry stand.
* **Deviation 2, 2026-11-12: the owner drops the tolerance test.**
  * Owner instruction, 2026-11-12, in the coordinator session, relayed by the coordinator: "There is no need for
    backwards compatibility engineering, I am the only user currently."
  * Decision: `TestStartAuthIgnoresAnOlderPhonesConfirmDestructive` and its planted break leave P24. Its row in step 4,
    its file in Files, its part of step 5's search and the wire-compatibility and rollout sentences that rested on it
    are struck, not deleted. The test had been written and seen failing on its plant (step 4's entry). It is removed
    from the tree, and `TestStartAuthPayloadHasNoConfirmDestructive`'s comment no longer points at it.
  * Not changed: `handleStartAuth` still decodes with `json.Unmarshal`, which ignores an unknown member, so an older
    phone's frame is still accepted; that is now untested, by this decision. The guide's line 1942 is unchanged.
  * Re-run after the removal, on the scratch clone with the step's files (the test file removed, the comment changed):
    see the entry below. The hosts are not run again: the removal deletes a test file and changes a comment, and no
    file the host runs compiled into the daemon changed since them.
* **Re-run, 2026-11-12, after deviations 1 and 2 were decided.**
  * `internal/ws/provider_start_auth_test.go` is removed from the tree. `TestStartAuthPayloadHasNoConfirmDestructive`'s
    comment no longer points at it. The scratch clone mirrors the tree: the step's 23 files and this PLAN, with nothing
    else changed.
  * Gates the removal touches, on that clone: `make pre-add-check FILES=…` (the 19 remaining Go files) "19 file(s)
    clean (gofmt, golint, govulncheck)"; `go test -count=1 ./internal/ws/...` ok; `go test -count=1
    ./internal/protocol/...` ok; `go vet` of both clean; `make lint` clean.
  * The other gates of step 6 stand from the first run, and so do the host runs. The Linux and Windows test hosts are
    not run again for removing one test file, which changes no code the daemon compiles.
  * Step 5's searches, on the final tree:
    * `git grep -n 'confirm_destructive\|confirmDestructive\|ConfirmDestructive' -- ':!docs'` finds three lines, all in
      `internal/protocol/provider_auth_test.go`: the test's name and its doc comment, which names the member.
    * `git grep -n 'ErrConfirmRequired\|confirm_required' -- ':!docs'` (deviation 1) finds the comment, the constant,
      the registry entry and the five senders, the same set as at `4614263c`.
  * Records: `python3 -B scripts/check_records.py --check-all` exit 0, no output; `make markdownlint-docs` "60 files,
    0 issues"; the redact preview "would edit 0 file(s)".
  * Staged: the step's 23 files and this PLAN, by path. Commit: the coordinator's.
