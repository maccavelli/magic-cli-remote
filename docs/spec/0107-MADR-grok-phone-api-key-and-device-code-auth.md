---
status: accepted
date: 2026-08-19
decision-makers: Project Owner (scope and acceptance); Implementer (measurement)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Phone card drives grok API-key and device-code auth; the host must not open a browser

## Context and Problem Statement

The owner re-authenticated **Codex** with an OAuth flow that shows a
**URL + one-time code**. They want that **on the phone**, for Grok:
a card with a tappable URL (phone browser) and a code to paste, which
authenticates the **host** grok process. The Mac must not be the
browser.

A first draft of this MADR treated the remaining work as stdout pins
because `xai:device` already exists. The owner rejected that: a
2026-08-19 isolated `grok login --device-auth` probe opened the
**system browser on this Mac** (`webbrowser::open`). They were not at
the machine; the xAI page timed out. That is the opposite of the
Codex phone card (`DeviceFlowSheet`: Open link / Copy code).

This record asks: **how does the phone drive grok API-key and RFC 8628
device-code auth the way it already drives Codex, without grok opening
a host browser, and without taking grok's loopback `--oauth`?**

This record asks: **what Codex actually did, what grok 1.0.5 already
exposes, what the phone already drives, and which remaining gaps must
close so grok's phone auth is the same two usable methods Codex has —
API key and OAuth-with-a-code — without taking grok's loopback
browser OIDC.**

It does **not** reopen [0106](./0106-MADR-grok-1.0.5-surface-parity.md)
(ACP `_meta` model/effort). It does **not** replace
[0085](./0085-MADR-grok-acp-auth-method-wiring.md) (spawn
`authenticate` selection and store correction). 0085 is still
`status: proposed` while much of D2/D4 already exists in the tree;
this MADR states the overlap and the leftover. It does **not** reopen
[0074](./0074-MADR-remote-provider-auth-from-phone.md) Strategy B
(reverse loopback tunnel).

### What Codex did (host, this machine)

* Binary: **codex-cli 0.147.0** at `/opt/homebrew/bin/codex`.
* `codex login --help` still has `--with-api-key`, `--with-access-token`,
  and `--device-auth` (empty help text). There is no separate
  "browser headless" flag.
* The working "URL + code in a browser" path is **`codex login
  --device-auth`**: RFC 8628 device authorization. Captured on
  0.146.0 (MADR 0074 D15, `providerauth/cli_test.go`
  `codexDeviceOutput`):

  ```text
  1. Open this link in your browser and sign in to your account
     https://auth.openai.com/codex/device
  2. Enter this one-time code (expires in 15 minutes)
     K5GK-PUGKG
  ```

  The user opens that URL on a phone or laptop, types the code,
  Codex polls, writes `~/.codex/auth.json`.
* Phone analog already shipped: `openai:api` (`codex login
  --with-api-key` on stdin) and `openai:device` (`login
  --device-auth`, D8-guarded because Codex **deletes** `auth.json`
  at start). Phone UI:
  `provider_detail_screen.dart` `_runDeviceSignIn` +
  `device_flow_sheet.dart` (system browser + displayed code,
  MADR 0086 D7).

### What grok 1.0.5 actually exposes

Probe target: **grok 1.0.5 (`5115b46bc909`) [stable]**. Sources:
`/Users/saxsmith/gitrepos/grok-build` crate version 1.0.5.
Isolated `GROK_HOME` (no `XAI_API_KEY`, no `auth.json`),
`grok login --device-auth`, killed after 8s (login not completed):

```text
To sign in, open this URL in your browser:

  https://accounts.x.ai/oauth2/device?user_code=PYGZ-C7A4

Confirm this code in your browser:

  PYGZ-C7A4

Only continue with a code you requested. Don't share it with anyone.

Waiting for authorization...
```

That is the **same class of flow as Codex**, not grok's `--oauth`
flag. Evidence in grok-build
`crates/codegen/xai-grok-shell/src/auth/device_code.rs`: RFC 8628
(`urn:ietf:params:oauth:grant-type:device_code`),
`verification_uri` + `user_code`, optional
`verification_uri_complete` (this probe's URL already embeds
`user_code=`). CLI prints URL then code to stderr and polls
(`prompt_and_poll`).

Grok's **other** login is different:

| Flag | Transport | Phone-reachable? |
| --- | --- | --- |
| `--device-auth` / `--device-code` | RFC 8628; URL + code; user completes at `accounts.x.ai` | **Yes** — same as Codex `--device-auth` |
| `--oauth` (default `grok login`) | Loopback OIDC at `auth.x.ai`, redirect `http://127.0.0.1/callback` (user guide 02-authentication.md) | **No** — completion hits the host (0074 FlowBrowser / Strategy B) |
| `XAI_API_KEY` / quoted `[model."<id>"] api_key` | No browser | **Yes** — file/env |

ACP `initialize.authMethods` (live 1.0.5 on this host, 0106 probe):
`xai.api_key`, `cached_token`, `grok.com`. `cached_token` is the
**result** of a completed device or browser login (`~/.grok/auth.json`),
not a phone button. `grok.com` is the hang/OIDC method 0085 D3 forbids
the daemon from auto-sending.

Grok TUI also speaks ACP extensions `x.ai/auth/get_url` (modes
`device` / `loopback` / `command`) and `x.ai/auth/submit_code`
(pager `effects/mod.rs`). That is **in-session TUI login**, not the
settings-sheet path Codex uses. Codex phone auth spawns the CLI.

### What the phone already advertises for grok

`internal/provider/grok/auth.go` `authStatus`:

| Phone method | Type | Drive path |
| --- | --- | --- |
| `xai:api` | `api_key` | `setCredential` → quoted `[model."<id>"]` (`credstore.SetGrokModelAPIKey`, comment cites 0085 D4) |
| `xai:device` | `oauth_device` | `startDeviceAuth` → `grok login --device-auth` (`device_auth.go`, non-destructive) |
| `xai:browser` | `oauth_browser` | **unusable** (`isUsable` is `available && !isBrowserOAuth`) |

The phone sheet already routes `oauth_device` through the same
`DeviceFlowSheet` Codex uses. So the *catalog* already matches
Codex's two usable methods. The question is completeness and
honesty, not a missing method type.

### Remaining gaps (facts)

1. **No live pin of grok 1.0.5 device-auth stdout.**
   `providerauth/cli_test.go` pins Codex 0.146.0 verbatim. There is
   no grok fixture. `scanForCode` would accept this probe
   (`urlPattern` on the `accounts.x.ai` line; `bareCodePattern` on
   `PYGZ-C7A4`). That is an untested assumption until a live test
   fails on a reformat.

2. **Host browser auto-open is observed, and it is the product bug.**
   grok-build `open_browser_detached` always calls `webbrowser::open`
   from `prompt_and_poll`, even when stdout/stderr are pipes. The
   2026-08-19 isolated probe did that on this Mac: the owner later
   found the system browser open at
   `https://accounts.x.ai/oauth2/device?user_code=PYGZ-C7A4` and
   the page timed out because they were not at the machine. The
   probe had already SIGTERM'd the CLI after 8s; login was not
   completed; `GROK_HOME` was a temp dir, so `~/.grok/auth.json`
   was not the store. Today's `startDeviceAuth` uses the same
   spawn (`device_auth.go:28-29`) with no env overlay, so a phone
   tap of `xai:device` will flash the **host** browser. The phone
   already has the Codex card (`DeviceFlowSheet`: Open link, Copy
   code, countdown). The missing work is: parse URL+code onto
   `oauth.device_flow`, **do not open the host browser**, let the
   user finish on the phone, host grok writes `auth.json`.

3. **0085 handshake is in the tree but the MADR is still proposed.**
   `acpagent.selectACPAuthMethod` + `SafeAuthMethodIDs` (`cached_token`,
   `xai.api_key`) + D7 `ErrACPAuthRequired` already run on spawn
   (`acpagent.go:519-539`, live Start logs `acp authenticated
   method_id=cached_token` in 0106 Phase B). Quoted model-table key
   write and `TestLiveQuotedModelKeyAdvertisesAPIKey` exist. What
   0085 still owns: treat the MADR as the spawn-handshake contract,
   fail-fast when nothing is safe, status that observes the same
   stores. This record does not re-decide D2/D4.

4. **API-key paste is not Codex-shaped.** Codex writes via
   `codex login --with-api-key` on stdin. Grok has no equivalent
   CLI; the accepted store is quoted `[model."<id>"] api_key` (0085
   isolated-home probe) or `XAI_API_KEY` (needs a process restart
   the daemon cannot perform). Phone already uses the file write.
   Completeness is: after `provider.set_credential`, grok
   `authenticate xai.api_key` and `session/new` succeed, and
   `AuthStatus` is configured. Live auth tests cover the store; a
   phone E2E pin is still the 0086 class of work.

5. **ACP `x.ai/auth/*` is not the phone path.** Driving get_url /
   submit_code would be a new in-session login UX. Codex does not
   do that from Settings. Matching Codex means keep spawning
   `grok login --device-auth`.

## Decision Drivers

* The live grok binary is the contract (AGENTS.md; 0050 D2; 0106).
* "OAuth browser headless with a code" on Codex **is** RFC 8628
  device authorization, not loopback OIDC. Name it that way so we
  do not implement `--oauth`.
* Headless-first (0074): no secret in argv; no mcremote vault; the
  phone shows URL + code; the CLI polls and writes grok's store.
* The phone is the browser. A host `webbrowser::open` is a product
  failure (observed 2026-08-19), not an acceptable side effect.
* Never auto-invoke ACP `grok.com` (0085 D3). Cold `authenticate
  grok.com` does not return in 8s.
* Every phone method is usable or marked unavailable up front
  (0083 D4). `xai:browser` stays `oauth_browser` / not usable.
* Do not silently sign the host out. Grok device-auth is
  non-destructive until success (`device_auth.go` comment); Codex
  remains D8-destructive.
* Pin grok's print format with a `live_grok` test the way Codex's
  output is pinned (0074 D15).

## Considered Options

* **O1 — Align grok's phone auth with Codex: API key + RFC 8628
  device-code (`grok login --device-auth`); keep `grok.com` /
  `--oauth` host-only; pin 1.0.5 stdout; close remaining
  completeness (parse fixture, spawn handshake after login,
  verify-after-write)**
* **O2 — Drive ACP `grok.com` / `x.ai/auth/get_url` + `submit_code`
  from the phone as a third method**
* **O3 — Strategy B reverse tunnel so `grok login --oauth` loopback
  can finish in the phone browser**
* **O4 — Do nothing: the catalog already lists `xai:api` and
  `xai:device`**

## Decision Outcome

Chosen option: **"O1 — Align grok's phone auth with Codex: API key +
RFC 8628 device-code; keep loopback host-only; pin 1.0.5; suppress
the host browser so the phone card is the only client"**, because
that is the flow the owner used on Codex, grok 1.0.5 already
implements it as `--device-auth`, the phone already has
`DeviceFlowSheet`, and leaving host `webbrowser::open` in place
makes the Mac the client again (observed).

This MADR is **accepted**. Companion plan:
[0107-PLAN-grok-phone-api-key-and-device-code-auth.md](0107-PLAN-grok-phone-api-key-and-device-code-auth.md)
(Complete, 2026-08-19). Phase commits: `0f854a0` (A, grok stdout
fixture), `826415a` (B, extra env + PATH stub + ExpiresIn 600),
`d228bde` (B2, darwin sandbox-exec wrap), `5f16bb4` (C, live
production StartDeviceAuth parse), `603ca5e` (D, catalog +
README). Phase E was run-only (unit, live_grok subset, race
green). Binary: grok 1.0.5 (`5115b46bc909`) [stable]. 0085
remains proposed.

### Sub-decisions (accepted)

**D1 — The Codex analog is `grok login --device-auth`, not
`grok login --oauth`.**

Phone method `xai:device` / `oauth_device` stays the only OAuth
the phone may start. URL + user code, daemon polls, credential
lands in `~/.grok/auth.json`. Next grok spawn advertises
`cached_token`; 0085 D2 authenticates it. Do not add a second
phone OAuth method.

**D2 — API key remains `xai:api` writing quoted
`[model."<current-default>"]` (0085 D4), not a new CLI.**

Grok has no `login --with-api-key`. Do not invent one. Keep
`CredentialModel` fill on `SetCredential`. After write, status
must observe that table (0085 D5; `auth.go` already checks
`HasGrokConfigAPIKey`). A write grok will not `authenticate` is
`credential_not_accepted` (0086), never "Credential saved".

**D3 — `xai:browser` / ACP `grok.com` / `--oauth` stay host-only.**

Same as 0085 D3. The phone continues to show the row disabled
(`oauth_browser`). This is **not** the code flow. Taking it from
the phone is Strategy B (0074 W3), a later MADR.

**D4 — Pin grok 1.0.5 device-auth stdout the way Codex 0.146.0 is
pinned.**

Add a verbatim fixture from the 2026-08-19 isolated probe (URL
`https://accounts.x.ai/oauth2/device?user_code=…`, code on its own
line). `StartCLIDeviceFlow` must return `FlowDevice` with that
URI and code. A live_grok test starts `grok login --device-auth`
under an isolated `GROK_HOME`, asserts parse, then kills the
process (do not complete login in CI). If grok reformats, the
test fails; we do not guess a new regex in production.

**D5 — Keep spawning the CLI; do not switch the phone path to
ACP `x.ai/auth/get_url`.**

Settings-sheet auth is pre-session, matching Codex. In-session
TUI login extensions stay unused.

**D6 — The phone is the only browser. Daemon-spawned grok
device-auth must not open a host browser.**

Observed 2026-08-19: piped `grok login --device-auth` opened
macOS Safari/Chrome to the device-code URL. `device_code.rs`
`open_browser_detached` always calls `webbrowser::open` and
has no `--no-browser`. On macOS that crate shells out to
`open` (docs.rs 1.0.6: `$BROWSER` is documented for
linux/unix, not macOS).

Product path (already built, Codex-identical):

1. Phone Settings → Grok → `xai:device` →
   `oauth.device_flow` card (`DeviceFlowSheet`).
2. User taps **Open link** (`url_launcher` on the phone) or
   copies the URL.
3. User copies the **code** from the card into that phone
   browser (or confirms the pre-filled `user_code=` query).
4. Host `grok login --device-auth` polls, writes
   `~/.grok/auth.json`. `oauth.device_flow_result` closes the
   card. Next grok spawn uses `cached_token` (0085 D2).

Implementation (original, still true on Linux): when the
daemon spawns `grok login --device-auth` for a phone flow,
overlay the child environment so grok's `open`/`xdg-open`
cannot launch a host browser. Process-scoped stub `open`
prepended to `PATH` that exits 0. Do not change the user's
login-shell PATH. Do not invent a grok CLI flag. A live test
must parse URL+code **and** leave the real host `auth.json`
unchanged; it must not require a human at the Mac display.

**D6 amendment (2026-08-19, after Phase B).** The PATH stub
does **not** suppress a host browser on this Mac. That was an
incorrect reading of webbrowser 1.0.6.

Facts (this host, grok 1.0.5 `5115b46bc909`, crate
`webbrowser` 1.0.6 from grok-build `Cargo.lock`):

* macOS path in the crate is
  `LSCopyDefaultApplicationURLForURL` then
  `LSOpenFromURLSpec` (Launch Services / CoreServices). It
  does **not** exec `open`, `/usr/bin/open`, or honour
  `$BROWSER`. `$BROWSER` is unix.rs only. Source:
  [webbrowser-rs v1.0.6 macos.rs](https://github.com/amodm/webbrowser-rs/blob/v1.0.6/src/macos.rs).
* grok is Developer ID signed (`X.AI Corporation`,
  `5Y6N3AJ54S`) with **hardened runtime**
  (`CodeDirectory flags=0x10000(runtime)`).
  `DYLD_INSERT_LIBRARIES` cannot interpose `LSOpenFromURLSpec`.
* PATH stub of `open`/`xdg-open` remains the Linux mechanism.
* `sandbox-exec` with only `deny mach-lookup` of
  `com.apple.lsd*` does **not** block https
  `LSOpenFromURLSpec` (C probe: status 0). Isolated
  `grok login --device-auth` under that profile still omitted
  grok's "Could not open browser automatically" line (so
  `webbrowser::open` returned Ok). Codes `FYPR-RKWJ` /
  `example.com/?mcremote-d6=probe` may have opened a tab
  during that measurement.
* `sandbox-exec` **deny-default**, no `mach-lookup`, allowing
  `process*`, `file-read*`, `file-write*`, `network*`,
  `sysctl-read`, `system-socket`, `file-ioctl`, `signal`:
  C probe `LSOpenFromURLSpec=-10827`; isolated grok printed
  URL+code **and** `(Could not open browser automatically —
  open the URL above manually.)` then `Waiting for
  authorization...`. Code `Q9ST-8NMR` was not completed.

Chosen macOS mechanism: wrap the grok **child only** as
`sandbox-exec -p <that profile> <grok-bin> login --device-auth`.
Do not sandbox the daemon. Do not invent a grok flag. Keep
the PATH stub for unix `xdg-open`. If this profile later
blocks writing `~/.grok/auth.json` on a real completion,
add `file-write*` (already in the profile) or named
`mach-lookup` entries **other than** `com.apple.lsd*`; never
`(allow mach-lookup)` blanket.

**D9 — Grok `DeviceFlow.ExpiresIn` is 600 seconds** (grok-build
`MIN_DEVICE_CODE_EXPIRY_FALLBACK_SECS` = 10 minutes) so the
phone countdown is not `"expired"` from the first frame.
Today `device_auth.go` omits `ExpiresIn` (zero). Codex sets
15 minutes. `DeviceFlowSheet` treats `expiresIn==0` as
expired. This is a one-line production fill, not a protocol
change.

**D7 — Grok device-auth stays non-destructive.**

No Codex-style confirm dialog. `device_auth.go` already states
grok leaves `auth.json` alone until exchange succeeds. A live
test must not start device-auth against the developer's real
`GROK_HOME`.

**D8 — Relationship to 0085.**

0085 remains the spawn-handshake and store-shape MADR (D1–D7
there). This MADR does not re-open those decisions. If 0085 is
still unaccepted, accept or execute it on its own PLAN; do not
silently fold remaining 0085 phases into 0107 without listing
them. 0107's execution is: D4 pin, D6 host-browser suppress on the
grok device-auth spawn, D9 expiry, plus confirmation that
phone `xai:api` / `xai:device` are the two usable grok methods
and `xai:browser` is not. No mobile UI rewrite — the card
already exists.

### Recommended uptake (implemented)

#### P1 — Pin 1.0.5 stdout and make the phone the only browser

1. Verbatim fixture + unit test in `providerauth/cli_test.go`
   (`TestParsesRealGrokDeviceOutput`) next to the Codex fixture.
2. Daemon grok `startDeviceAuth` stops the host browser (D6):
   extra env PATH stub (Linux `xdg-open`); on darwin, wrap the
   child with `sandbox-exec -p` deny-default (no `mach-lookup`)
   so `LSOpenFromURLSpec` fails and grok prints the URL+code
   for the phone. Extend `StartCLIDeviceFlow` with extra env;
   Codex keeps `nil`.
3. `DeviceFlow.ExpiresIn = 600` (D9).
4. `live_grok` test: isolated `GROK_HOME`, same spawn as
   production (including suppress env), parse URL+code, Kill.
   Real `~/.grok/auth.json` unchanged. Must not require a human
   at the Mac.

#### P2 — Catalog honesty (unit, no protocol change)

3. `auth.go` comments and `auth_test.go`: usable methods are
   `xai:api` and `xai:device`; `xai:browser` remains
   `oauth_browser`. Fail if someone "fixes" browser to usable
   without a Strategy B MADR.

#### P3 — Confirm, do not rewrite, the two drive paths

4. API key: existing `SetGrokModelAPIKey` +
   `TestLiveQuotedModelKeyAdvertisesAPIKey`. No new store.
5. Device: existing `startDeviceAuth` argv
   `login --device-auth`. Change argv only if P1's live parse
   fails.

#### P4 — Explicitly not taken

| Surface | Why not |
| --- | --- |
| `grok login --oauth` / ACP `grok.com` from the phone | Loopback / hang. 0085 D3 / 0074 W3 |
| ACP `x.ai/auth/get_url` + `submit_code` | TUI in-session login |
| `GROK_FORCE_LOGIN_TEAM_ID` | Host login policy, 0106 P3 |
| Injecting `XAI_API_KEY` into launchd | 0085 out of scope; file write is the phone path |
| Codex-style destructive confirm on grok | Grok does not delete `auth.json` at start |
| Strategy B reverse tunnel | Own MADR |

## Consequences

* Good, because the owner's Codex experience maps onto a grok
  flag that already exists and a phone sheet that already
  renders URL + code.
* Good, because we do not confuse `--oauth` with `--device-auth`
  and take a hang.
* Neutral, because 0085's handshake MADR is still proposed even
  though spawn already authenticates `cached_token` /
  `xai.api_key`.
* Neutral, because grok's CLI still *tries* `webbrowser::open`;
  we intercept it per-child rather than waiting for an xAI flag.
* Bad, because without P1 a grok stdout reformat would show the
  phone a wrong or empty code, the same class of lie 0074 D15
  exists to catch for Codex.

### Confirmation

A P1 item is not done until its named tests exist and are green.
Live tests: `//go:build live_grok`, skip when `grok` is not on
`PATH`, isolated `GROK_HOME`, never complete a real login in
unit tests. `make pre-add-check` on every touched Go file.

| ID | Decision | Test | File | Tag | Must fail when |
| --- | --- | --- | --- | --- | --- |
| T-G1 | D4 | `TestParsesRealGrokDeviceOutput` | `providerauth/cli_test.go` | unit | fixture URL/code not parsed, or ANSI leaks |
| T-G2 | D4/D6 | `TestLiveGrokLoginDeviceAuthParses` | `internal/provider/grok/live_auth_test.go` | `live_grok` | isolated production spawn prints no parseable code, real `~/.grok/auth.json` changes, darwin spawn is not `sandbox-exec`, or a host browser still opens |
| T-G3 | D6 | `TestGrokDeviceAuthSuppressesHostOpen` | `internal/provider/grok/device_auth_test.go` | unit | child env PATH does not start with the stub `open` dir, or Codex spawn grows the overlay |
| T-G5 | D6 | `TestGrokDeviceAuthDarwinSandboxWrap` | `device_auth_test.go` | unit | on darwin, spawn argv is not `sandbox-exec -p <deny-default profile> <bin> login --device-auth`, or the profile allows blanket `mach-lookup` |
| T-G4 | D9 | `TestGrokDeviceFlowExpiry` | `device_auth_test.go` | unit | `ExpiresIn` is 0 |
| T-C1 | D1/D3 | `TestGrokAuthMethodsUsableSet` | `internal/provider/grok/auth_test.go` | unit | `xai:browser` is usable, or `xai:device` is not `oauth_device`, or `xai:api` is missing |
| T-C2 | D2 | existing `TestSetGrokModelAPIKeyWritesQuotedTable` | `credstore/write_test.go` | unit | write returns to `[auth]` |
| T-A | D8 | existing live quoted-key authenticate | `live_auth_test.go` | `live_grok` | `authenticate xai.api_key` fails on a quoted table |

## Pros and Cons of the Options

### O1 — API key + device-code, pin 1.0.5, keep loopback out

* Good, because it is what Codex actually is, and what grok
  1.0.5 prints under `--device-auth`.
* Good, because the phone sheet already implements it.
* Neutral, because much of the drive path is already written;
  the new work is pins and honesty, not a third OAuth type.
* Bad, because operators who wanted `grok.com` from the phone
  still see a disabled row.

### O2 — ACP `grok.com` / `x.ai/auth/*` from the phone

* Good, because it would reuse grok's TUI login RPCs.
* Bad, because cold `grok.com` hangs, and `submit_code` is a
  TUI paste box for loopback/device internals, not Settings.
* Bad, because Codex Settings does not work that way, so it
  would not match the requested analog.

### O3 — Strategy B tunnel for `--oauth`

* Good, because `--oauth` is grok's default interactive login.
* Bad, because 0074 already deferred it as an order of magnitude
  more machinery, and it is not what Codex `--device-auth` is.

### O4 — Do nothing

* Good, because `xai:api` and `xai:device` already exist.
* Bad, because grok device-auth stdout is unpinned, so the next
  CLI reformat is a silent phone failure.
* Bad, because the owner's request ("like Codex") would be
  answered by a catalog that already lists the methods without
  proving the 1.0.5 wire still parses.

## More Information

### Method

* Codex: `codex --version` 0.147.0; `codex login --help`;
  existing fixture `codexDeviceOutput` (0.146.0). Did **not**
  run `codex login --device-auth` on this host (D8).
* Grok: `grok --version` 1.0.5 (`5115b46bc909`);
  `grok login --help`; isolated `GROK_HOME` +
  `grok login --device-auth` 8s capture (login not completed).
  Owner later confirmed the system browser opened to that
  `user_code` URL and timed out while they were away.
* grok-build: `device_code.rs` (RFC 8628, print, `webbrowser::open`);
  `flow.rs` `AuthUrlMode::{Device,Loopback,Command}`;
  pager `x.ai/auth/get_url` / `submit_code`.
* Ours: `grok/auth.go`, `grok/device_auth.go`,
  `codex/auth.go`, `codex/device_auth.go`,
  `providerauth/cli.go` + `classify.go`,
  `acpagent/authselect.go`,
  `apps/mobile/.../provider_detail_screen.dart`,
  `device_flow_sheet.dart`.

### Related

* [0074](./0074-MADR-remote-provider-auth-from-phone.md) —
  Strategy A device-code; Strategy B deferred. Its accepted D20–D29 amendment
  and [approved P17–P22 plan](./0074-PLAN-remote-provider-auth-from-phone.md)
  supersede this record's direct-to-LIVE Grok credential completion and cleanup
  with isolated pending login, conditional publication, generations,
  method-specific logout, and owned flow lifecycle; host-browser suppression
  from this record remains required
* [0083](./0083-MADR-provider-auth-activation-and-layout-gaps.md) —
  usable vs browser_only
* [0085](./0085-MADR-grok-acp-auth-method-wiring.md) —
  ACP authenticate + quoted key store (still proposed)
* [0086](./0086-MADR-phone-provider-auth-completion.md) —
  tap-to-open device URL; completion honesty
* [0106](./0106-MADR-grok-1.0.5-surface-parity.md) —
  1.0.5 ACP session `_meta`; auth catalog unchanged
