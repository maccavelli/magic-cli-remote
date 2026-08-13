<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# Implement make phone-driven provider auth actually complete

Associated MADR: [0083-MADR-provider-auth-activation-and-layout-gaps.md](0083-MADR-provider-auth-activation-and-layout-gaps.md)

| field | value |
| --- | --- |
| status | **accepted** 2026-08-13; implementation in progress, phase by phase. |
| phases | P1 bottom-inset correctness (D1) · P2 error taxonomy (D5) · P3 thread method+inputs (D2) · P4 opencode device dialect (D3) · P5 per-method availability in the catalog (D4) |
| rule | Commit per phase; do not push until asked. Each phase leaves daemon and app releasable and interoperable with older peers. |

## Goal

Close MADR 0083's two defect classes: every interactive row reachable on
edge-to-edge Android, and every auth method the UI offers either completes
from the phone or is disabled up front with a stated reason — with typed
inputs delivered to the engines, device flows working on both
OpenCode-family agents, and errors that say what to do next.

## Scope

In scope: `apps/mobile`, `internal/provider/*`, `internal/ws`,
`internal/protocol`, `docs/protocol-v1.md`. Out of scope: goose OS-keyring
writes (MADR 0083 D6 — its own future record), the browser-OAuth loopback
tunnel (0074 W3), and any engine-side change (both engines are used as
shipped).

## Grounding facts (verified 2026-08-12)

| # | Fact | Evidence |
| --- | --- | --- |
| G1 | **The engine write API has a home for typed inputs.** opencode 1.18.16's OpenAPI (`GET /doc`, probed live) defines `PUT /auth/{providerID}` taking `Auth = OAuth \| ApiAuth \| WellKnownAuth`, where `ApiAuth = {type:"api", key, metadata?: map[string]string}` (`additionalProperties:false` — nothing else is accepted). Device flows run via `POST /provider/{providerID}/oauth/authorize {method: <index>, inputs?: map[string]string}` — present on **both** engines (kilo 7.4.21 probed in 0074 §7.1; opencode probed today). | live `GET /doc` dump, `scratchpad/oc-doc.json` |
| G2 | All five `SetCredential` implementations bind `methodID`/`inputs` to `_`: `opencode/auth.go:199`, `kilo/auth.go:112`, `goose/auth.go:153`, `grok/auth.go:22`, `codex/auth.go:29`. The opencode/kilo bodies are hard-coded `{"type":"api","key":…}` (`opencode/auth.go:209`, `kilo/auth.go:122`). `SetCredentialFile` (restart path) has the same gap (`opencode/auth.go:244`). | cited lines |
| G3 | Typed methods in both engines' catalogs are `type:"api"` **with prompts** (azure `resourceName`, cloudflare `accountId`/`gatewayId`, kilo azure `endpointType` select with conditional fields) — i.e. the prompt answers are ApiAuth `metadata`, not a different auth type. | `internal/provider/opencode/testdata/provider-auth-1.18.16.json`, `kilo/testdata/provider-auth-7.4.20.json` |
| G4 | Only kilo implements `httpagent.DeviceAuthDialect` (`kilo/device_auth.go:36`); it already passes `inputs` (`:53-55`), resolves method ids to engine indexes via `methodIndexOf` (`:131`), and polls 5 s/15 min for the credential to appear. `httpagent.StartDeviceAuth` falls back to `ErrAuthUnsupported` for dialects without it (`httpagent/httpagent.go:241-249`). | cited lines |
| G5 | Daemon error surface: `writeAuthErr` (`internal/ws/server.go:2261`) maps busy/unsupported/confirm-required; the residual is `credential_failed` + clipped raw error. New codes must be registered or `TestWSErrorCodesAreRegistered` fails (`internal/ws/error_codes_test.go:69`). `ValidateSecret` yields `ErrEmptySecret`/`ErrSecretTooLarge` (`credstore/write.go:21-26`); goose refusal is `ErrGooseKeyringManaged` (`credstore/write.go:210`); keyring detection is `credstore.GooseKeyringDisabled(cfgPath)` (used at `goose/auth.go:168`). | cited lines |
| G6 | Phone error surface: `friendlyOpError` has no provider-auth cases and passes `e.message` through (`apps/mobile/lib/data/ws/mc_exception.dart:31-49`); the detail screen's credential catch blocks call `showTopNotification` without `severity:` and its default is `NoticeSeverity.info` (`theme/top_notification.dart:80-83`). | cited lines |
| G7 | Layout: edge-to-edge is enforced (targetSdk = Flutter default 35+, `android/app/build.gradle:56`; no nav-bar config in `styles.xml`). Fixed bottom paddings: hub ListView `bottom: 24` (`settings_screen.dart`), Providers `vertical: 8` (`providers_screen.dart:117`), detail `bottom: 24` (`provider_detail_screen.dart:145`, last row = Add credential). The catalog sheet's chrome is `PickerSheetLayout` only, which pads `viewInsets` (keyboard), not `viewPadding` (system bar) (`option_picker_sheet.dart:61-79`); its pre-0082-P4 build had `bottomInsetFor` + `SafeArea(top:false)`, and `bottomInsetFor` still exists (`provider_auth_sheet.dart:25`). Detail-screen sheet opens pass no `useSafeArea` (`provider_detail_screen.dart:395,423,546`). | cited lines |
| G8 | Wire model: `AuthMethodPayload` (`internal/protocol/messages.go:512`) and the phone's `AuthMethod.fromJson` (`models.dart`) both parse tolerantly — **additive fields are ignored by old peers**, so D4's annotation needs no version bump; the whole surface is already gated on the `provider_auth` capability. | messages.go, models.dart |
| G9 | The auth sheet defaults to the first non-browser method (`provider_auth_sheet.dart:66-75`); catalog rows render browser-only methods disabled with a reason chip already (`upstream_catalog_sheet.dart`, "Host only"), which is the pattern D4 extends. | cited lines |
| G10 | Live evidence of what works today: plain api-key round-trip on opencode (live test, `MCREMOTE_LIVE_AUTH_WRITE=1`), kilo gateway device flow (0074 §7.1), codex stdin login, grok config write. | 0074 §14, MADR 0083 A8 |

## Implementation Steps

Gates for every phase: `go build ./... && go test ./... && go vet ./...`,
`gofmt -l cmd internal` empty; `cd apps/mobile && dart format --output=none
--set-exit-if-changed . && dart analyze && flutter test`. Commit per phase.

### P1 — Bottom-inset correctness (MADR D1; fixes L1–L6)

1. **New helper** in `apps/mobile/lib/features/settings/section_card.dart`
   (exported alongside `SettingsSection`):

   ```dart
   /// Bottom padding for a scrollable that must clear the system bar on
   /// edge-to-edge Android (MADR 0083 L1): the inset plus breathing room.
   EdgeInsets listBottomPadding(BuildContext context, {double extra = 24}) =>
       EdgeInsets.only(bottom: MediaQuery.viewPaddingOf(context).bottom + extra);
   ```

2. Replace the three fixed paddings (G7): hub ListView and detail ListView →
   `padding: listBottomPadding(context)`; Providers ListView →
   `padding: listBottomPadding(context, extra: 8).copyWith(top: 8)`.
3. Catalog sheet: wrap the `Expanded(child: _body(theme))` content in
   `SafeArea(top: false, child: …)` inside the existing `PickerSheetLayout`
   column (`upstream_catalog_sheet.dart`), restoring what 0082 P4 dropped.
4. The three `showModalBottomSheet` calls in `provider_detail_screen.dart`
   (`:395,423,546`) gain `useSafeArea: true` (parity with
   `showOptionPicker`).
5. Card density (L6): in `providers_screen.dart` move the credential-summary
   `Text` out of the chip `Wrap` onto its own subtitle line with
   `maxLines: 1, overflow: TextOverflow.ellipsis`; chips stay on line one.
6. **Tests** (`test/providers_screen_test.dart`, `provider_detail_screen_test.dart`,
   `upstream_catalog_sheet_test.dart`): a shared harness sets
   `tester.view.padding` (via `FakeViewPadding(bottom: 96)`) and asserts the
   last interactive row's bottom edge ≤ `viewport height − 96/dpr` after
   `scrollUntilVisible` — i.e. the row is tappable clear of the inset; plus
   one assertion that the catalog sheet's list honours the padding
   (`tester.getBottomLeft` of the last row).
7. Gate + commit.

### P2 — Error taxonomy first (MADR D5; fixes A5)

Done before the wiring phases so P3/P4 failures are debuggable in the field.

1. **Daemon** (`internal/ws/server.go` `writeAuthErr`, G5): add cases —
   `errors.Is(err, credstore.ErrGooseKeyringManaged)` → code
   `keyring_managed`, message "this agent keeps its keys in the host's OS
   keyring; add the key on the host"; `errors.Is(err,
   provider.ErrAuthMethodUnsupported)` (new sentinel in
   `internal/provider/auth.go`, introduced here, returned from P3/P4) →
   `method_unsupported`, "this sign-in method can't be driven from the
   phone for this agent"; `errors.Is(err, credstore.ErrEmptySecret)` or
   `ErrSecretTooLarge` → `invalid_key` with the validator's text;
   `errors.Is(err, context.DeadlineExceeded)` on the engine call →
   `engine_unavailable`, "the agent's engine did not answer; is it running
   on the host?". Residual stays `credential_failed`. All codes registered
   for `TestWSErrorCodesAreRegistered` (G5).
2. **Phone** (`mc_exception.dart` `friendlyOpError`): one sentence per new
   code (G6), keeping the daemon's message as the fallback for
   `credential_failed`.
3. **Severity**: every catch block in `provider_detail_screen.dart` that
   reports a failed auth operation passes `severity: NoticeSeverity.error`.
4. **Tests**: Go table test for `writeAuthErr` code mapping; Dart unit test
   for `friendlyOpError` on the four new codes; widget test that a failing
   `setProviderCredential` shows an error-styled toast (fake client throws
   `McException(code: 'keyring_managed')`).
5. Gate + commit.

### P3 — Thread `methodID` and `inputs` end to end (MADR D2; fixes A1)

1. **httpagent engine body** (`opencode/auth.go:199`, `kilo/auth.go:112`):
   both `SetCredential`s take the real `methodID, inputs` and build the body
   per G1:

   ```go
   body := map[string]any{"type": "api", "key": secret}
   if len(inputs) > 0 {
       body["metadata"] = inputs
   }
   ```

   The method's *type* guards the path: resolve the method from the cached
   auth catalog (`fetchAuthMethods`, already present on both dialects); an
   `oauth`-typed methodID sent to `SetCredential` returns
   `provider.ErrAuthMethodUnsupported` (P2's sentinel) instead of writing an
   api-shaped credential for an oauth method. An empty/unknown `methodID`
   keeps today's behaviour (plain api key) — that is the D16 long-tail path
   and must not regress.
2. Same change to `opencode/auth.go:244` `SetCredentialFile`
   (restart-write path): merge `metadata` into the auth.json entry via a new
   `credstore.MergeJSONAuthWithMetadata` (same atomic 0600 write).
3. **goose/grok/codex** keep ignoring `inputs` — their methods declare none
   (G3 applies to engine agents only) — but stop ignoring `methodID`: a
   non-empty methodID that is not their sole advertised method returns
   `ErrAuthMethodUnsupported` rather than silently writing the wrong store.
4. **Conformance**: extend `internal/provider/auth_conformance_test.go` —
   for each `AuthWriter`, calling `SetCredential` with a bogus oauth-typed
   methodID yields `ErrAuthMethodUnsupported`, and (httpagent fakes) the
   engine body carries `metadata` exactly when inputs are non-empty.
5. **Unit tests with engine fakes** (existing `auth_test.go` harnesses in
   both packages): azure-shaped write (`resourceName`) asserts the PUT body
   `{"type":"api","key":…,"metadata":{"resourceName":"my-models"}}`.
6. **Live test** (tagged, opt-in like G10's): round-trip a scratch typed
   vendor on the local opencode engine with metadata, assert
   `GET /config/providers` reflects it, then delete.
7. Gate + commit.

### P4 — OpenCode device-auth dialect (MADR D3; fixes A2)

1. **New file** `internal/provider/opencode/device_auth.go`: mirror
   `kilo/device_auth.go` (G4) — same `POST /provider/{id}/oauth/authorize`
   body `{method: <index>, inputs}`, same D7 URL-shape classification of the
   response, same 5 s / 15 min poll for the credential to appear in
   `GET /config/providers`, same cancel func. Extract the shared pieces
   (`methodIndexOf`, poll loop) into `httpagent/deviceauth.go` if the mirror
   is >80 % identical — one implementation, two dialect shims — otherwise
   keep the copy with a header comment linking the two.
2. `internal/provider/auth_conformance_test.go`: opencode now expects
   `wantDeviceOAuth: true`.
3. **Live test** `opencode/live_auth_test.go`: start a device flow for a
   vendor advertising an oauth method, assert a URL+code come back within
   the timeout, then cancel — no completion required (no real account), the
   assertion is that the flow *starts* where it previously returned
   `unsupported`.
4. **Phone**: no change needed — `_runDeviceSignIn` is provider-generic; the
   codex-only destructive guard stays keyed on `providerId == 'codex'`.
5. Gate + commit.

### P5 — Per-method availability in the catalog (MADR D4; fixes A3/A4/A6)

1. **Protocol** (`internal/protocol/messages.go:512`): `AuthMethodPayload`
   gains `Available *bool` + `Reason string` (omitempty; absent = available,
   so old daemons read as all-available on new phones and vice versa — G8).
   Document in `docs/protocol-v1.md`'s 0074 table.
2. **Daemon annotation** at catalog/status build time (`internal/ws`
   `upstreamAuthPayload`, plus goose's `authCatalog`):
   * goose on a keyring-managed host (`!credstore.GooseKeyringDisabled`,
     G5): every api-key method → `available:false, reason:"keyring_managed"`;
   * any `oauth_device` method on a provider whose concrete type does not
     implement `provider.DeviceAuth` → `available:false,
     reason:"device_unsupported"` (after P4 this bites nothing on the
     engine agents, but keeps the invariant for future providers);
   * `oauth_browser` methods → `available:false, reason:"browser_only"`
     (moves today's client-side heuristic to the daemon as the source of
     truth; the client heuristic stays as fallback for old daemons).
3. **Phone model** (`models.dart` `AuthMethod`): parse `available`
   (default true) and `reason`; `UpstreamAuth` gains
   `bool get hasUsableMethod`.
4. **Phone UI**: catalog rows disable when no method is usable, with the
   reason chip ("Host only · keyring" for `keyring_managed`, existing "Host
   only" for `browser_only`); `ProviderAuthSheet.initState` picks the first
   **available** method (fixes A3, G9) and renders unavailable methods in
   the dropdown disabled with their reason; the submit button never enables
   for an unavailable method.
5. **Tests**: Go — payload annotation table (goose keyring on/off, device
   with/without dialect); Dart — a keyring-managed goose row is disabled
   with the reason; a vendor with `[oauth (unavailable), api_key]` defaults
   the sheet to api_key; old-daemon fallback (no fields) keeps today's
   behaviour.
6. Gate + commit.

## Verification

Whole-plan acceptance, after P5:

1. Both full suites green (Go + Flutter), `gofmt` clean, analyzer clean.
2. Live suites: opencode/kilo auth round-trips including one typed-input
   (metadata) write; opencode device-flow start; catalog annotation counts
   logged.
3. **Real-device checklist** (Android, gesture nav, five-agent host):
   * every last row (hub, Providers, detail, catalog sheet) tappable;
   * plain key vendor (togetherai on kilo) end to end;
   * typed vendor (azure on opencode) — inputs land, status flips to
     configured;
   * device flow on an opencode oauth vendor reaches the code sheet;
   * goose vendor shows "Host only · keyring" before any input is possible;
   * a deliberate bad write shows an error-styled, actionable toast.
4. Interop: new phone ↔ pre-P5 daemon (fields absent → all methods usable,
   today's behaviour); old phone ↔ new daemon (extra fields ignored).

## Rollout and Rollback

* One commit per phase; any prefix is releasable. P1/P2 are pure client/
  error-surface fixes — revert-safe individually. P3 changes write bodies:
  its live tests gate the commit, and a revert restores the old (lossy)
  bodies without breaking stored credentials — `metadata` is additive in
  the engine store. P4 is a new file plus one conformance flip. P5 is
  additive protocol; rollback is field removal, old peers never see it.
* No stored-format migrations anywhere; no engine restarts beyond what the
  existing write paths already do.
* The goose keyring limitation remains by decision (MADR 0083 D6) and is
  user-visible from P5 onward instead of failing after secret entry.
