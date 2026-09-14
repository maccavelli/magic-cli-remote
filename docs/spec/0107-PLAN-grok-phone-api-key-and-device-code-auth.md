# Implement phone-card grok device-code auth (host must not open a browser)

Associated MADR: [0107-MADR-grok-phone-api-key-and-device-code-auth.md](0107-MADR-grok-phone-api-key-and-device-code-auth.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: **Complete** (2026-08-19). Rewritten the same day after
  the owner rejected a pin-only draft. Amended the same day after
  Phase B: PATH stub does not stop macOS Launch Services (D6
  amendment). Phases: `0f854a0` (A), `826415a` (B), `d228bde`
  (B2, darwin `sandbox-exec`), `5f16bb4` (C, live production
  parse), `603ca5e` (D, catalog + README). Phase E run-only:
  unit, `live_grok` subset (including
  `TestLiveGrokLoginDeviceAuthParses`), and race green. Binary:
  grok **1.0.5 (`5115b46bc909`)** [stable].
- **Date**: 2026-08-19
- **Keyed to**: grok **1.0.5 (`5115b46bc909`) [stable]**. If
  `grok --version` differs, **stop** and update MADR 0107.
- **Scope**: `internal/providerauth/cli.go` (optional extra env on
  spawn), `internal/provider/grok/device_auth.go` (suppress host
  `open`, `ExpiresIn`), tests, `README.md` one sentence, this pair.
  **No mobile rewrite** (`DeviceFlowSheet` already is the card).
  **No protocol change.** **No 0085 handshake work.** Do not edit
  Codex device-auth beyond passing `nil` extra env if the
  `StartCLIDeviceFlow` signature grows.

**Accepted lifecycle follow-up:** [MADR 0074 §15](0074-MADR-remote-provider-auth-from-phone.md)
and [0074-PLAN P17–P22](0074-PLAN-remote-provider-auth-from-phone.md) retain this
plan's Linux PATH stub and Darwin sandbox browser suppression, but supersede the
direct live `GROK_HOME` mutation, bare wait closure, completion, cancellation,
backup, and logout lifecycle.

## Goal

When the user taps Grok → Sign in with xAI (device code) on the
phone, they get the **same card Codex already shows**: a URL they
can open in the **phone** browser, a code they can copy into it,
and a waiting state until the **host** grok process writes
`~/.grok/auth.json`. The Mac must **not** open a browser.

That is not a new phone UI. It is: parse grok 1.0.5
`login --device-auth` stdout, put URI+code on `oauth.device_flow`,
stop grok's `webbrowser::open` from hitting the host, fill
`ExpiresIn` so the card does not say `expired` immediately.

## MADR assessment (grounded)

Owner correction (same day): the Mac browser tab from the
isolated probe is the failure mode, not an acceptable side
effect. D6 is now **suppress host open**, not document it.

### What is already built (do not rebuild)

| Piece | Where |
| --- | --- |
| Phone card: Open link, Copy link, Copy code, wait | `apps/mobile/lib/features/settings/device_flow_sheet.dart` |
| Phone starts grok device method | `provider_detail_screen.dart:513-518` `_runDeviceSignIn` |
| WS `oauth.device_flow` / `_result` / cancel | `internal/ws/server.go:2279-2346` |
| Grok spawn argv | `grok/device_auth.go:29` `login --device-auth` |
| Parser | `providerauth/cli.go` `scanForCode` |
| Codex analog | `codex/device_auth.go` same helper, destructive D8 |

### What is wrong today

| Bug | Fact |
| --- | --- |
| Host is the browser | grok `prompt_and_poll` → `webbrowser::open`; 2026-08-19 owner saw the tab; `startDeviceAuth` passes no extra env |
| Card says expired | `DeviceFlow{Interval:5}` omits `ExpiresIn`; sheet `_remaining<=0` → `"expired"` |
| Stdout unpinned | no grok fixture next to `codexDeviceOutput` |

### What this plan must not be read as

* Pin-only / comments-only (rejected).
* `grok login --oauth` or ACP `grok.com` from the phone.
* New Flutter screens.
* Folding 0085 handshake into this pair.

## Scope

### In scope

D4 pin, D6 host-browser suppress on the grok child only (PATH
stub + darwin `sandbox-exec`), D9 `ExpiresIn=600`, T-G1–T-G5,
T-C1, README sentence, live parse through **production**
`StartDeviceAuth`.

### Out of scope

`--oauth`, `x.ai/auth/get_url`, Strategy B, 0085 remaining
phases, Codex D8, mobile UI, protocol fields, inventing a grok
`--no-browser` flag.

## Plan-level decisions

1. **Extra env on `StartCLIDeviceFlow`, Codex passes nil.**

   ```go
   func StartCLIDeviceFlow(ctx context.Context, bin string, args []string, scanTimeout time.Duration, extraEnv []string) (Classification, *CLIFlow, error)
   ```

   When `len(extraEnv)>0`, build `cmd.Env` from `os.Environ()`
   with overlay keys replaced (not blindly appended — PATH must
   win). When empty, leave `cmd.Env` nil (inherit, today's
   Codex behaviour). Update every existing
   `StartCLIDeviceFlow(` call: Codex and `cli_test.go` pass
   `nil`.

2. **Suppress host browser on the grok child only, not
   `$BROWSER` alone.** webbrowser 1.0.6 uses `$BROWSER` /
   `xdg-open` on linux/unix and **Launch Services
   (`LSOpenFromURLSpec`)** on macOS — not `/usr/bin/open`
   (MADR 0107 D6 amendment, measured 2026-08-19).

   Linux: Grok helper in `device_auth.go`:

   ```go
   // hostOpenStubDir writes Unix executables named "open" and
   // "xdg-open" that exit 0. Caller deletes the dir.
   func hostOpenStubDir() (dir string, extraEnv []string, err error)
   ```

   `extraEnv` is exactly `[]string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}`.
   `startDeviceAuth` creates the dir, passes extraEnv into
   `StartCLIDeviceFlow`, and `t.Cleanup`/defer-remove after
   `Wait`/`Kill` (the child must still exist while grok polls).
   **Do not** stub `open` on the daemon's own PATH.

   Darwin (Phase B2): wrap argv as
   `sandbox-exec -p <grokDeviceAuthSandbox> <bin> login --device-auth`
   via `wrapGrokDeviceAuth(bin)`. Profile is deny-default with
   no `mach-lookup` (so `LSOpenFromURLSpec` fails) and allows
   `process*`, `file-read*`, `file-write*`, `network*`,
   `sysctl-read`, `system-socket`, `file-ioctl`, `signal`.
   Do not `(allow mach-lookup)` blanket. PATH stub still
   applies. `DYLD_INSERT_LIBRARIES` is not viable (grok
   hardened runtime). If `sandbox-exec` is missing, fail
   `startDeviceAuth` on darwin rather than spawn unsandboxed.

3. **`ExpiresIn: 600`** on the returned `provider.DeviceFlow`
   (grok-build `MIN_DEVICE_CODE_EXPIRY_FALLBACK_SECS`). Codex
   stays 15 minutes.

4. **Live T-G2 calls production `StartDeviceAuth`**, not a raw
   `StartCLIDeviceFlow`, so suppress env is actually used.

   ```go
   p := grok.New(grok.Config{})
   da, ok := any(p).(provider.DeviceAuth)
   flow, wait, err := da.StartDeviceAuth(ctx, "xai", "xai:device", nil, false)
   ```

   Then `Kill` via cancelling wait's context (the wait func
   already calls `flow.Kill` on ctx done — `CLIFlow.Wait`).
   Snapshot real `os.UserHomeDir()+"/.grok/auth.json"` **before**
   `isolateGrokHome`.

5. **Fixture** is the 2026-08-19 capture (dead code `PYGZ-C7A4`):

   ```text
   To sign in, open this URL in your browser:

     https://accounts.x.ai/oauth2/device?user_code=PYGZ-C7A4

   Confirm this code in your browser:

     PYGZ-C7A4

   Only continue with a code you requested. Don't share it with anyone.

   Waiting for authorization...
   ```

6. **If live parse is red: stop**, amend MADR, do not guess a
   regex. If suppress still opens a host browser: stop, amend D6
   (do not ship a PATH stub or sandbox wrap that does not work
   on this Mac). Measured 2026-08-19: PATH stub cannot intercept
   `LSOpenFromURLSpec`; darwin wrap is Phase B2.

7. One phase → one commit. `git commit --no-edit`. No push.
   `make pre-add-check FILES="…"` before staging Go.

## Tests

| ID | Function | File | When |
| --- | --- | --- | --- |
| T-G1 | `TestParsesRealGrokDeviceOutput` | `providerauth/cli_test.go` | Phase A |
| T-G3 | `TestGrokDeviceAuthSuppressesHostOpen` | `grok/device_auth_test.go` **new** | Phase B |
| T-G4 | `TestGrokDeviceFlowExpiry` | same | Phase B |
| T-G5 | `TestGrokDeviceAuthDarwinSandboxWrap` | same | Phase B2 |
| T-G2 | `TestLiveGrokLoginDeviceAuthParses` | `grok/live_auth_test.go` | Phase C |
| T-C1 | `TestGrokAuthMethodsUsableSet` | `grok/auth_test.go` | Phase D |
| T-C2 | existing `TestSetGrokModelAPIKeyWritesQuotedTable` | `credstore/write_test.go` | Phase E run-only |
| T-A | existing `TestLiveQuotedModelKeyAdvertisesAPIKey` | `live_auth_test.go` | Phase E run-only |

Existing tests that must stay green:
`TestParsesRealCodexDeviceOutput`, `TestGarbledOutputFailsCleanly`,
`TestAuthStatusIncludesBrowserMethod`,
`TestSetCredentialGuardsMethodID`.

Update every `StartCLIDeviceFlow(` in `cli_test.go` to pass `nil`
as the new last argument.

## Implementation Steps

### Ground rules

1. One phase → one commit. Do not push.
2. `make pre-add-check FILES="…"`.
3. `git commit --no-edit`.
4. Never complete a grok login in a test. Never run live
   device-auth against the real `GROK_HOME`.
5. Do not edit 0085, mobile Dart, or Codex production logic.

### Verified baseline

| Claim | Evidence |
| --- | --- |
| 1.0.5 device-auth stdout | isolated probe; URL embeds `user_code` |
| Host browser opened | owner observed; D6 |
| Phone card already Codex-shaped | `device_flow_sheet.dart:179-218` |
| Grok `ExpiresIn` is 0 | `device_auth.go:33-36` |
| Sheet treats 0 as expired | `device_flow_sheet.dart:107-111` |
| Production argv | `["login", "--device-auth"]` |

---

### Phase A — Pin grok 1.0.5 stdout (unit only)

**MADR:** D4, T-G1
**Files:** `internal/providerauth/cli_test.go`
**No production change. No live grok. No host browser.**

1. Add `grokDeviceOutput` after `codexDeviceOutput`.
2. `TestParsesRealGrokDeviceOutput` using `fakeCLI` +
   `StartCLIDeviceFlow` (after Phase B the call takes `nil` env;
   in Phase A the signature is still the old one **unless** you
   combine A+B — **do not**. Phase A uses the **current**
   signature. Phase B updates all call sites).

Wait: if A lands first without extraEnv, B changes the
signature and must update A's new test too. That is fine: B
touches `cli_test.go` to add `nil` at every call including
T-G1.

#### Verification

```bash
make pre-add-check FILES="internal/providerauth/cli_test.go"
go test ./internal/providerauth/ -count=1
```

#### Acceptance

T-G1 green. Codex fixture still green.

---

### Phase B — Extra env + suppress stub + expiry (production)

**MADR:** D6, D9, T-G3, T-G4
**Files:**
`internal/providerauth/cli.go`,
`internal/providerauth/cli_test.go` (nil extraEnv at every call),
`internal/provider/codex/device_auth.go` (`nil` only),
`internal/provider/grok/device_auth.go`,
`internal/provider/grok/device_auth_test.go` **new**.

#### Production

1. `StartCLIDeviceFlow(..., extraEnv []string)` as decision 1.
2. `hostOpenStubDir` in `device_auth.go` as decision 2. Scripts:

   ```sh
   #!/bin/sh
   exit 0
   ```

   mode `0755`, names `open` and `xdg-open`.
3. `startDeviceAuth`:
   * `dir, extra, err := hostOpenStubDir()`
   * `defer os.RemoveAll(dir)` **after** Wait/Kill — hold the
     dir for the life of the child. Pattern:

     ```go
     dir, extra, err := hostOpenStubDir()
     if err != nil { return ... }
     cls, flow, err := providerauth.StartCLIDeviceFlow(ctx, bin, []string{"login", "--device-auth"}, deviceCodeScanTimeout, extra)
     if err != nil {
         _ = os.RemoveAll(dir)
         return ...
     }
     wait := func(wctx context.Context) error {
         defer os.RemoveAll(dir)
         return flow.Wait(wctx)
     }
     ```

     If `StartCLIDeviceFlow` fails after creating `flow`, Kill
     then remove. Document that a leaked dir is under `os.TempDir()`.
4. Return `ExpiresIn: 600`, `Interval: 5`, URI and code from
   `cls`.

#### Tests

* T-G3: `hostOpenStubDir` returns a dir containing executable
  `open` and `xdg-open`; extra env is a single `PATH=` whose
  first element is that dir. Do not spawn grok.
* T-G4: cannot call `startDeviceAuth` without grok. Test the
  returned struct by extracting a small `deviceFlowFromParse(cls)`
  helper used by `startDeviceAuth`:

  ```go
  func grokDeviceFlow(cls providerauth.Classification, wait func(context.Context) error) provider.DeviceFlow {
      return provider.DeviceFlow{VerificationURI: cls.VerificationURI, UserCode: cls.UserCode, ExpiresIn: 600, Interval: 5}
  }
  ```

  (wait is separate return). T-G4 asserts `ExpiresIn==600` on
  `grokDeviceFlow(Classification{...}, nil)` — wait, DeviceFlow
  doesn't hold wait. Just:

  ```go
  func grokDeviceFlowResult(cls providerauth.Classification) provider.DeviceFlow
  ```

  `startDeviceAuth` uses it. T-G4: empty cls still gets
  `ExpiresIn==600`.

* All previous `StartCLIDeviceFlow` tests pass with `nil`.

#### Verification

```bash
make pre-add-check FILES="internal/providerauth/cli.go internal/providerauth/cli_test.go internal/provider/codex/device_auth.go internal/provider/grok/device_auth.go internal/provider/grok/device_auth_test.go"
go test ./internal/providerauth/ ./internal/provider/grok/ ./internal/provider/codex/ -count=1
```

#### Acceptance

Grok child PATH is stub-first. Codex spawn env still inherited.
Expiry is 600.

**Committed:** `826415a`.

---

### Phase B2 — Darwin sandbox wrap (production)

**MADR:** D6 amendment, T-G5
**Files:** `internal/provider/grok/device_auth.go`,
`internal/provider/grok/device_auth_test.go`.
**Do not** change `StartCLIDeviceFlow` again. **Do not** run
T-G2 until this lands — PATH-only spawn still opens the host
browser on this Mac.

#### Production

1. Constant `grokDeviceAuthSandbox` (exact profile, no
   blanket `mach-lookup`):

   ```
   (version 1)
   (deny default)
   (allow process*)
   (allow signal)
   (allow file-read*)
   (allow file-write*)
   (allow file-ioctl)
   (allow sysctl-read)
   (allow system-socket)
   (allow network*)
   ```

2. Helper:

   ```go
   func wrapGrokDeviceAuth(bin string) (spawnBin string, args []string, err error)
   ```

   Returns `bin, []string{"login", "--device-auth"}, nil` on
   non-darwin. On darwin, `exec.LookPath("sandbox-exec")`; if
   missing, return an error (do not spawn unsandboxed). Else
   `spawnBin=sandbox-exec`,
   `args=["-p", grokDeviceAuthSandbox, bin, "login", "--device-auth"]`.

3. `startDeviceAuth` uses `wrapGrokDeviceAuth(bin)` then
   `StartCLIDeviceFlow(ctx, spawnBin, args, …, extra)`. Keep
   `hostOpenStubDir` as today.

#### Tests (T-G5)

* Darwin: `wrapGrokDeviceAuth("/opt/fake/grok")` spawnBin
  contains `sandbox-exec`; args equal
  `-p`, profile, `/opt/fake/grok`, `login`, `--device-auth`.
  Profile contains `(deny default)` and does not contain
  `(allow mach-lookup`.
* Non-darwin: spawnBin is the given bin; args are
  `login --device-auth`.
* Darwin smoke: `sandbox-exec -p grokDeviceAuthSandbox /usr/bin/true`
  exits 0 (profile parses). Do **not** call `/usr/bin/open`
  or grok in this unit test.

#### Verification

```bash
make pre-add-check FILES="internal/provider/grok/device_auth.go internal/provider/grok/device_auth_test.go"
go test ./internal/provider/grok/ ./internal/providerauth/ -count=1
```

#### Acceptance

On this Mac, production spawn is `sandbox-exec -p … grok login --device-auth`.
Linux PATH stub unchanged. No live grok in this phase.

---

### Phase C — Live production StartDeviceAuth parse (no host login)

**MADR:** D4, D6, T-G2
**Files:** `internal/provider/grok/live_auth_test.go`

Only if Phase B **and B2** are green and `grok --version` is
1.0.5.

This test **must not** open a usable host browser. If a browser
still appears, **stop** and amend D6 (do not ship a darwin
wrap that still lets `LSOpenFromURLSpec` succeed). Success
signal from the 2026-08-19 probe: grok prints `(Could not
open browser automatically — open the URL above manually.)`
while still printing URI+code. T-G2 does not have to assert
that stderr line (production `StartDeviceAuth` only returns
the parsed flow); it must use production `StartDeviceAuth`
so the wrap is actually on the child.

#### Steps

`TestLiveGrokLoginDeviceAuthParses`:

1. Snapshot real `filepath.Join(userHome, ".grok", "auth.json")`
   via `os.UserHomeDir()` **before** `isolateGrokHome`.
2. `isolateGrokHome(t)`.
3. `p := grok.New(grok.Config{})`; skip if `!p.Ready()`.
4. `da, ok := any(p).(provider.DeviceAuth)`; fail if !ok.
5. ctx timeout 45s.
6. `flow, wait, err := da.StartDeviceAuth(ctx, "xai", "xai:device", nil, false)`.
7. Always `defer` a cancel that runs `wait(cancelledCtx)` or
   document that `wait(ctx)` with cancelled ctx kills the CLI
   (`CLIFlow.Wait` → `Kill`). Pattern:

   ```go
   waitCtx, waitCancel := context.WithCancel(ctx)
   defer waitCancel()
   defer func() { _ = wait(waitCtx); waitCancel() }()
   ```

   Simpler: after asserts, `waitCancel()` then
   `_ = wait(waitCtx)` so Kill runs.
8. Assert `flow.UserCode != ""`, URI contains `accounts.x.ai`
   and `flow.UserCode`, `flow.ExpiresIn == 600`.
9. Re-stat real auth.json; fail if created/changed.

#### If red

**Stop.** Named fatal. Amend MADR. No regex guess. If the host
browser still opens, the PATH stub failed on this OS — amend D6
with the observed `open` path (`which open` inside the child).

#### Verification

```bash
make pre-add-check FILES="internal/provider/grok/live_auth_test.go"
go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokLoginDeviceAuthParses -count=1 -timeout 60s
```

#### Acceptance

Production grok device-auth returns a phone-displayable URI+code
without mutating real `auth.json`. Implementer does **not** need
to sit at the Mac.

---

### Phase D — Catalog honesty + README

**MADR:** D1, D3, T-C1
**Files:** `auth.go` comments, `device_auth.go` comments,
`auth_test.go`, `README.md` under `## Provider: Grok`.

1. Comments: `xai:device` is the Codex analog (0107 D1);
   `xai:browser` host-only (D3); spawn suppresses host `open`
   (D6); 1.0.5 pin.
2. `TestGrokAuthMethodsUsableSet`: three methods, types as in
   MADR. Keep `TestAuthStatusIncludesBrowserMethod` (provider
   layer `Unavailable==false` so the WS annotator can set
   `browser_only`).
3. README: phone can paste `xai:api` or start device-code
   `xai:device` (card: open URL on the phone, copy code). Host
   browser is not used. `--oauth` stays host-only.

#### Verification

```bash
make pre-add-check FILES="internal/provider/grok/auth.go internal/provider/grok/device_auth.go internal/provider/grok/auth_test.go"
go test ./internal/provider/grok/ ./internal/providerauth/ -count=1
```

---

### Phase E — Run-only

```bash
go test ./internal/providerauth/ ./internal/provider/grok/ ./internal/provider/codex/ ./internal/provider/credstore/ -count=1
go test -tags live_grok ./internal/provider/grok/ -run 'TestLiveGrokLoginDeviceAuthParses|TestLiveQuotedModelKeyAdvertisesAPIKey|TestLiveInitializeAuthMethodsHost|TestLiveColdInitializeOnlyGrokCom' -count=1 -timeout 180s
go test -race ./internal/providerauth/ ./internal/provider/grok/ -count=1
```

If T-A (quoted-key authenticate) is red → 0085, not this plan.

---

### Phase F — Close the pair

MADR `status: accepted`, this PLAN **Complete** with SHAs
above. Do not mark 0085 accepted. Do not push.

## Verification (plan-wide)

| Gate | Command |
| --- | --- |
| Unit | `go test ./internal/providerauth/ ./internal/provider/grok/ ./internal/provider/codex/ -count=1` |
| Live | T-G2 via production `StartDeviceAuth` |
| Race | before Phase F |
| Binary | grok 1.0.5 |

### Acceptance criteria

1. Phone Grok → device sign-in yields `oauth.device_flow` with a
   tappable `accounts.x.ai` URL and a user code (existing
   `DeviceFlowSheet`).
2. Host does not need a person at the Mac display; stub `open`
   is on the **child** PATH (Linux); darwin wraps the child in
   `sandbox-exec` so `LSOpenFromURLSpec` fails.
3. `ExpiresIn==600` so the card counts down.
4. Real `~/.grok/auth.json` unchanged by tests.
5. `xai:browser` still not usable.
6. No mobile/protocol/0085 edits. No push.

## Rollout and Rollback

### Rollout

No flag. After deploy, phone Grok device-code uses the existing
card. Completing the code in the **phone** browser writes host
`~/.grok/auth.json`; next grok session authenticates
`cached_token`.

### Rollback

Revert F→A. Reverting B restores host browser flash and
`ExpiresIn==0`. Drive path still exists either way.

## More Information

* Phone card (do not rewrite): `device_flow_sheet.dart`
* WS push: `handleStartAuth` → `TypeOAuthDeviceFlow`
* Grok CLI: `webbrowser::open` in `device_code.rs:365,396`
* 0085 remains separate
* Transactional successor: [MADR 0074 §15](0074-MADR-remote-provider-auth-from-phone.md)
  / [0074-PLAN P17–P22](0074-PLAN-remote-provider-auth-from-phone.md)
