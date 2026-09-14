<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

# Implement MADR 0074 — Remote provider auth from phone (W1 + W2 + W4 + W5)

Associated MADR: [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md)

- **Status**: **implemented** — P1–P11 landed 2026-08-11/12 (commits `a5c408b`,
  `58906a6`, `e604146`, `03d435c`, `9429f5a`); **P12–P15 landed 2026-08-12** and
  are the full-vendor-coverage wave (MADR 0074 D16–D19). P16 records what is
  deliberately left. **Approved repair amendment 2026-08-21:** P17–P22
  implement accepted D20–D29 and have not begun. Verified
  against the tree, not against commit messages — see §0.2 and §17.
- **Date**: 2026-08-10, revised 2026-08-12; repair approved 2026-08-21
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
| P18 | **Complete** — `ff3bb65`, `c02b66a`, `3508b4c`, `8778dac`, `88a4bb1`, `2eb8f0b`, `076b3f4` | `go test -count=1 ./internal/...` fully green; `-race` green on codex, grok, credstore, providerauth, ws; `make pre-add-check` clean on every changed file | All 14 steps; both adapters, the shared owned-flow engine, both coordinated providers, the logout split, and the API-key transaction | Still dark: no production daemon call site constructs a coordinated provider, so the pre-P20 device-auth path is what executes. P20 activates it. |
| P19 | **Complete** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Watcher, reconciliation, operator CLI, and `RecoverAll`; `fsnotify` promoted to a direct dependency (it is the inotify wrapper on Linux, and a watcher that cannot start degrades to reconciliation). | |
| P20 | **Complete** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Reservation-based registry, owned server flow, disconnect/resume, shutdown drain, additive wire contract, daemon activation, CLI registration. **This is the phase that made the fix live.** | |
| P21 | **Complete** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Idempotent cancellation behind every dismissal path, transactional copy, `ready_to_activate` rendering, per-method removal. | |
| P22 | **Complete except the acceptance run** | `make test`, `make race`, `make vet`, `make pre-add-check` green; Flutter format/analyze/test green (1041 tests) | Live-tagged isolation tests for both providers, convergence-under-kills test, operator recovery guide, full matrix. | `make live-codex` and `make live-grok` both green 2026-08-21; a `live-grok` target was added, since the phase referenced one that never existed. Real credentials verified byte-identical before and after. Three pre-existing `live_grok` turn-behaviour failures reproduce at `9276ada` and are unrelated. The completed-login, rotation, busy-activation, and logout run is left for the owner: it spends tokens and revokes a real grant. |

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
   the pending Codex environment. For Grok, launch its ACP transport in the
   pending environment, complete only `initialize` plus cached-token
   authentication, then shut it down before creating a session or prompt. Bound
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
