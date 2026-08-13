<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# MADR 0083 — Make phone-driven provider auth actually complete: activation gaps and bottom-inset layout defects

| field | value |
| --- | --- |
| status | **accepted** 2026-08-13, **implemented** 2026-08-13 (D1–D5; D6 remains the named goose-keyring follow-up). |
| related | MADR 0074 (remote provider auth), MADR 0082 (settings/provider UX overhaul, shipped in v0.10.6), MADR 0073 (active-upstream escape), `docs/standards/mobile/flutter.md` |
| evidence | First real-device use of v0.10.6 (Android, gesture navigation) plus the code audit below; every finding carries a file:line citation verified 2026-08-12 |
| plan | [0083-PLAN-provider-auth-activation-and-layout-gaps.md](0083-PLAN-provider-auth-activation-and-layout-gaps.md) (proposed, drafted 2026-08-12 for joint review) |

## Context and Problem Statement

v0.10.6 shipped the MADR 0082 provider surfaces. First use on a physical
Android phone against a live five-agent host surfaced two defect classes:

1. **Layout**: the Providers screen's cards and the provider sheets run
   underneath the Android system navigation area, and the bottommost rows —
   including *Add credential*, deliberately placed last — cannot be tapped.
2. **Activation**: many credential set-ups do not complete. The user pastes a
   key or starts a sign-in and receives an ambiguous failure toast.

The second class is the one that matters most: MADR 0074's entire purpose is
that **auth can be driven from the phone**. This record establishes, from the
code, exactly which paths can succeed today, which cannot, why, and what has
to change so that every method the UI offers either works or honestly says —
*before* the user types a secret — why it cannot.

## Findings — layout (class L)

| # | Finding | Evidence |
| --- | --- | --- |
| L1 | The app targets `flutter.targetSdkVersion` (SDK 35+ on Flutter 3.44), so **edge-to-edge is enforced on Android 15/16**: the gesture/nav area is transparent and content draws beneath it. Nothing in the app opts out or compensates globally. | `android/app/build.gradle:43,56`; `styles.xml` has no nav-bar config |
| L2 | `ProvidersScreen`'s card list uses `padding: EdgeInsets.symmetric(vertical: 8)` — no `viewPadding.bottom`, no `SafeArea`. The last agent card sits under the nav bar; its `onTap` is occluded. | `providers_screen.dart:117` |
| L3 | `ProviderDetailScreen`'s ListView uses a fixed `padding: EdgeInsets.only(bottom: 24)`. The gesture inset on current devices is larger than 24 px, and the **last row is `Add credential`** — the row 0082 D16 exists for is the one that is obscured. | `provider_detail_screen.dart:145`, add-credential tile is the final child |
| L4 | The settings hub ListView has the same fixed `bottom: 24`. | `settings_screen.dart` hub `ListView` |
| L5 | The catalog sheet **lost its bottom inset in 0082 P4**. Its previous build wrapped content in `Padding(bottom: bottomInsetFor(mq))` + `SafeArea(top: false)`; the rebuilt chrome is `PickerSheetLayout` only, which handles the keyboard (`viewInsets`) but not the system bar (`viewPadding`) — `option_picker_sheet.dart:61-79`. The 0079 pickers themselves survive because `PickerCatalogView` wraps its action row in `SafeArea(top: false)` and `showOptionPicker` passes `useSafeArea: true`; the catalog sheet's ListView rows do not, and the detail screen's three `showModalBottomSheet` calls pass no `useSafeArea` (`provider_detail_screen.dart:395,423,546`). | cited files |
| L6 | Card height: each provider card is a `ListTile` with a 32 px leading icon plus a `Wrap` subtitle stacking a chip and a summary string; on a narrow device the Wrap breaks to extra lines, making every card taller than designed and pushing more content under the bar. | `providers_screen.dart:126-152` |

## Findings — activation (class A)

The write path, end to end: phone (`ProviderAuthSheet` → `setProviderCredential`)
→ daemon `handleSetCredential` (`internal/ws/server.go:2034`) → provider
`SetCredential`.

| # | Finding | Evidence |
| --- | --- | --- |
| A1 | **`methodID` and `inputs` are dropped by every provider.** All five `SetCredential` implementations bind them to `_`: opencode (`opencode/auth.go:199`), kilo (`kilo/auth.go:112`), goose (`goose/auth.go:153`), grok (`grok/auth.go:22`), codex (`codex/auth.go:29`). The phone's dynamic form (`provider_auth_sheet.dart`) faithfully collects the typed inputs 0074 D5 modelled — GitHub Copilot's deployment type, GitLab's instance URL, Azure's resource name — and the daemon discards them. The engine write is always `{"type": "api", "key": …}` regardless of the chosen method (`opencode/auth.go:209`, `kilo/auth.go:122`). Every typed-method vendor therefore "saves" a wrong-shaped credential or fails at the engine — 8 of kilo's 13 typed vendors need inputs (0074 §5). | cited lines |
| A2 | **OpenCode has no device-auth wiring at all.** Only kilo implements `httpagent.DeviceAuthDialect` (`kilo/device_auth.go:36`); for opencode, `httpagent.StartDeviceAuth` falls through to `ErrAuthUnsupported` (`httpagent/httpagent.go:241-249`). Every opencode catalog method classified `oauth_device` — which `ClassifyCatalogMethod` produces for any engine `oauth` method without a browser marker (`httpagent/authcatalog.go`) — fails the moment the user taps *Start sign-in*, with the toast "unsupported for this provider". The popular vendors are exactly the ones with oauth methods (Anthropic/Claude Pro, ChatGPT, Copilot), which is why the failure reads as "most setups don't activate". | cited lines |
| A3 | **The auth sheet defaults to the broken path.** `ProviderAuthSheet.initState` picks the *first* non-browser method (`provider_auth_sheet.dart:66-75`); engines commonly list the oauth method before the API-key method, so for those vendors the pre-selected flow is the (unwired, per A2) device flow, and the working API-key path is hidden behind the method dropdown. | cited lines |
| A4 | **Every goose write refuses on a default host.** goose stores secrets in the OS keyring unless `GOOSE_DISABLE_KEYRING` is set; `setCredential` returns `ErrGooseKeyringManaged` (`goose/auth.go:166-171`, `credstore/write.go:210`) — deliberate per 0074 D18, but the catalog still renders all 73 goose vendors with an enabled "API key" chip. The user types a key, submits, and only then learns nothing can be stored. |
| A5 | **Errors reach the phone raw and un-actionable.** `writeAuthErr` (`server.go:2261`) maps only `busy`/`unsupported`/`confirm-required`; everything else becomes code `credential_failed` with a clipped Go error chain ("opencode set credential for x: PUT /auth/x: …"). The phone's `friendlyOpError` has no case for any provider-auth code and passes the message through verbatim (`mc_exception.dart:31-49`), and the detail screen's failure toasts don't set `severity: NoticeSeverity.error` (`provider_detail_screen.dart` catch blocks), so failures render in the info style. |
| A6 | **The catalog overpromises.** `BuildCatalog` assigns a default API-key method to every vendor the engine lists (`httpagent/authcatalog.go`, "the path that makes togetherai … configurable") — correct for engine-backed agents — but no row is validated against what *the daemon on this host* can actually drive: device methods without a dialect (A2), goose's keyring-managed store (A4). Browser-only rows are already rendered disabled-with-reason ("Host only"); the same honesty is missing for these two classes. |
| A7 | **The kilo device flow proves A1 is an oversight, not a design**: `StartDeviceAuth` *does* pass `inputs` through to the engine (`kilo/device_auth.go:53-55`). The parameter plumbing exists on one path and was dropped on the other. |
| A8 | What verifiably works today: plain API-key writes on opencode and kilo engines (live round-trip test, 2026-08-12, `MCREMOTE_LIVE_AUTH_WRITE=1`); codex `login --with-api-key` via stdin (`codex/auth.go:29-51`); grok config-file key writes (`grok/auth.go:22-37`); the kilo gateway device flow (0074 §7.1 probe). The failure surface is concentrated in typed-input vendors (A1), opencode oauth methods (A2/A3), and all of goose (A4). |

## Decision Drivers

* **The 0074 goal is binding**: every auth method the UI offers must either
  complete from the phone or be visibly, pre-emptively marked as
  host-only/with-reason — never fail after the user has typed a secret.
* **No regression of the 0074 security posture**: no secret in argv, no
  mcremote-owned vault (D2), 0600 atomic writes, no key material in errors.
* **Honest UI over silent degradation** (the same principle as D16's
  truncation disclosure and the browser-only "Host only" rows).
* **Every interactive element reachable** on edge-to-edge Android with
  gesture nav — a hard usability floor, and a `flutter.md` obligation.
* Small, testable increments; each fix independently shippable.

## Considered Options

* **O1 — Layout-only hotfix**: fix insets (L1–L6), leave activation as is.
* **O2 — Full activation pass**: fix insets *and* close the wiring gaps —
  thread `methodID`/`inputs` end to end, add opencode device auth, surface
  per-method drivability in the catalog, and map errors to actionable copy.
* **O3 — O2 plus goose keyring writes** via a native keychain API (no argv),
  removing the last "cannot store from phone" class.

## Decision Outcome

Chosen option: **"O2 — full activation pass"**, because O1 repairs the
symptom the user can see while leaving the product's core promise broken,
and O3's keychain write is a genuinely new security surface (platform
keychain APIs, cgo, goose's single-blob store format) that deserves its own
record rather than a rider on a bug-fix MADR. O3 is recorded as the explicit
follow-up for the goose gap; until then goose rows become honest (D4) instead
of failing late.

### Sub-decisions

**D1 — Bottom-inset correctness everywhere (fixes L1–L6).**
The three `ListView`s (hub, Providers, detail) pad by
`MediaQuery.viewPaddingOf(context).bottom + 24` instead of a constant; the
catalog sheet restores a `SafeArea(top: false)` around its list (keeping
`PickerSheetLayout` for the keyboard half); the detail screen's three
`showModalBottomSheet` calls pass `useSafeArea: true`. Card density: the
provider card's summary string moves out of the chip `Wrap` onto its own
line with `maxLines: 1` + ellipsis, bounding card height. A widget test pins
the regression: with a simulated `viewPadding.bottom`, the last row's rect
must not intersect the inset region.

**D2 — `methodID` and `inputs` are threaded through every write (fixes A1).**
`SetCredential` signatures already carry them; implementations stop binding
them to `_`. For opencode/kilo the engine body is built from the method: the
typed method's declared input keys are sent alongside the key in the shape
the engine's `PUT /auth/{id}` accepts for that method type (to be confirmed
against the engine source the way 0074 §5 confirmed the read side, and
covered by a live round-trip test per typed vendor class). A method whose
type the daemon cannot fulfil returns a **typed refusal**
(`ErrAuthMethodUnsupported`) instead of writing a wrong-shaped credential.

**D3 — OpenCode gets the device-auth dialect kilo already has (fixes A2).**
`kilo/device_auth.go` is dialect-generic: POST authorize + poll for the
credential to appear. The same engine API exists on opencode 1.18.16 (both
are the OpenCode-family server). Implement `opencode` `StartDeviceAuth` as a
mirror, with the same D7 URL-shape classification and polling bounds, and a
live-tagged test against the local engine.

**D4 — The catalog tells the truth per method, per host (fixes A4/A6).**
The daemon annotates each catalog method with whether *it* can drive it:
`available: false` + `reason` (`keyring_managed`, `device_unsupported`,
`browser_only`) on `AuthMethodPayload`. The phone renders unavailable
methods the way browser-only rows already render — visible, disabled, with
the reason chip — and the auth sheet's default method becomes *the first
method the daemon marked available* (fixes A3). goose rows on a
keyring-managed host therefore read "Host only · keyring" up front, and the
user never types a key into a dead end. Wire change is additive and
capability-gated exactly like the rest of 0074 (older phones ignore the new
fields).

**D5 — An error taxonomy the phone can speak (fixes A5).**
`writeAuthErr` gains cases: `keyring_managed`, `method_unsupported`,
`engine_unavailable`, `invalid_key` (from `ValidateSecret`), keeping
`credential_failed` as the true residual. `friendlyOpError` maps each to one
actionable sentence ("goose keeps its keys in the host's keyring — run
`goose configure` there", "this sign-in type isn't supported for this agent
yet — use an API key", …), and every credential-failure toast uses
`NoticeSeverity.error`. Raw engine text stays in the daemon log (0069 D7),
not on the phone.

**D6 — goose keyring writes are a named follow-up, not silently deferred.**
The options (macOS Security.framework via cgo; Linux Secret Service via
D-Bus; goose gaining a first-party non-interactive credential command
upstream) each avoid argv but add real surface. A future MADR decides; this
record's D4 makes the current limitation visible instead of erroneous.

### Consequences

* Good, because after D2+D3 the two engine-backed agents — 369 of the 444
  catalog rows across the five agents — can complete both API-key and
  device flows from the phone, and the remainder fail *before* secret entry
  with a stated reason, which is the 0074 promise made real.
* Good, because D1 restores the hard usability floor and pins it with a
  test; D5 turns a screenshot-and-guess support loop into copyable reasons.
* Neutral, because D4 adds two fields to a capability-gated payload — old
  phones and old daemons interoperate unchanged.
* Bad, because D2's engine body-shapes need a per-method-type verification
  pass against the engine source plus live tests — real work, and the part
  most likely to surface further engine quirks.
* Bad, because goose remains read-only on default hosts until D6's
  follow-up lands; D4 only makes that honest, not better.

### Confirmation

* Widget tests: inset regression test (D1); unavailable-method rendering and
  default-method selection (D4); severity + copy per error code (D5).
* Go tests: `SetCredential` passes method/inputs through (unit, per
  provider); typed refusal for undrivable methods (D2); opencode
  `StartDeviceAuth` live test (D3); `writeAuthErr` code table (D5).
* Live acceptance on a real Android device against the five-agent host:
  every bottom row tappable with gesture nav; one typed-input vendor
  (e.g. Azure via kilo), one device-flow vendor on opencode, and one plain
  key vendor each complete end to end; a goose row shows the keyring reason
  before any input is possible.

## More Information

Per-agent activation matrix as the code stands today (the "before" column
this record exists to change):

| agent | plain API key | typed-input methods | device OAuth | notes |
| --- | --- | --- | --- | --- |
| opencode (184) | ✅ engine PUT | ❌ inputs dropped (A1) | ❌ no dialect (A2) | popular vendors are oauth-first (A3) |
| kilo (185) | ✅ engine PUT | ❌ inputs dropped (A1) | ✅ engine flow | inputs *do* flow on the device path (A7) |
| goose (73) | ❌ keyring refusal (A4) | — | — | works only with `GOOSE_DISABLE_KEYRING` |
| codex (1) | ✅ `login --with-api-key` | — | ✅ (destructive-guarded) | |
| grok (1) | ✅ config write | — | ✅ | |
