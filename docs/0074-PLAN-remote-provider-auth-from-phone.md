<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

# Implement MADR 0074 — Remote provider auth from phone (W1 + W2 + W4)

Associated MADR: [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md)

- **Status**: proposed — not yet implemented.
- **Date**: 2026-08-10
- **Scope**: Workstreams **W1** (auth status, credential injection, active-upstream
  switch), **W2** (device OAuth, Strategy A), and **W4** (polish) from MADR 0074 §11.
  **W3 (loopback tunnel) is explicitly out of scope** and belongs to a successor
  MADR; no `oauth.loopback_tunnel_*` message may be added by this plan.
- **Standing rule (repo)**: every phase that writes or modifies code scopes its
  tests as explicit numbered Steps, never as passive Acceptance prose. Commit
  per phase; do not push until asked.
- **Hard gate**: phases P7–P10 (W2) do not start until the **W1 exit gate**
  (after P6) is green. P11 (W4) may run after P6.

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
D14 active-upstream switch · D15 live-tagged tests pin every auth surface.

### 0.2 Host credential stores this plan writes (D1)

| agent | path / mechanism | write method |
| --- | --- | --- |
| kilo | daemon-owned engine | `PUT /auth/{providerID}` `{type:"api", key}` |
| opencode | `~/.local/share/opencode/auth.json` | direct write, 0600, atomic |
| goose | `~/.config/goose/config.yaml` + OS keyring | direct write, 0600, atomic |
| codex | `~/.codex/auth.json` | `codex login --with-api-key` on stdin |
| grok | `~/.grok/config.toml` per-model `api_key` | direct write, 0600, atomic |

**Grok note.** D1 permits `XAI_API_KEY` in the service environment, but that
needs a service restart the daemon cannot perform on itself. This plan writes
`~/.grok/config.toml` instead; the env path stays an operator option.

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

**Standing safety rule for anyone running these probes**: snapshot every
credential store to a 0600 sidecar first. The MADR's own research deleted a live
codex credential; it was restored only because a backup existed.

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
