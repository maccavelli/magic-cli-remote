# PLAN 0066: Secure-storage upgrade resilience — implementation plan

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: **Implemented 2026-08-02** — P0–P5 landed, one commit per
  phase; Flutter and Go suites green throughout. Hardware rows E1–E3
  outstanding in [ops-hardware-validation.md](ops-hardware-validation.md).
  Implements
  [0066-MADR-secure-storage-upgrade-resilience.md](0066-MADR-secure-storage-upgrade-resilience.md)
  D1–D9: round-1 D1–D6, plus the round-2 additions (D8 host
  observability, D9 fingerprint tile, D4 amendments) folded in
  2026-08-02 after **D7 was decided as Option A** — Keystore placement
  stays, so P0–P2 are unchanged in storage mechanics.
- **Date**: 2026-08-02
- **Scope**: Phone app (`apps/mobile`), two ops docs, and — for D8 — one
  log line in `internal/ws` plus one `pair list` column in
  `internal/cli`. No relay, protocol, or CI changes. D1 (keep the 10.3.1
  backend) and D7 (keep Keystore placement) require no code at all.

---

## 0. Grounding — where every change lands

Verified against the working tree (post-`a883f57`) and the installed plugin
source in the pub cache.

### 0.1 The seams that already exist

| Fact | Where | Used by |
|---|---|---|
| `SettingsStore` injects `secure`, `prefs`, `allowPlaintextFallback`, `clock` | `settings_store.dart:37-53` | every P0 unit test |
| Secret r/w/c helpers with fail-closed mobile semantics | `_readSecret :700`, `_writeSecret :726`, `_clearSecret :761` | P0 probe + marker |
| Token API | `getToken :263`, `setToken :265`, `clearToken :268` | P0 marker lifecycle |
| `clearAll` clears cwd prefs it should not (MADR F9) | `settings_store.dart:801-833`, cwd removals `:811-813` | P0 step 4 |
| In-memory-only failure record | `_lastSecureFailure`, `settings_store.dart:130`, `_recordSecureFailure` | P0 step 6 (persist it) |
| Secure-storage test fakes | `settings_store_test.dart:866` `_FailingSecureStorage`, `:872` `_ControlledSecureStorage`, `:925` `_InMemorySecureStorage` | P0 tests |
| Cold-start load + auto-connect | `connect_screen.dart` `_loadSaved` (~`:440-500`): reads host+token, then dials | P1 probe call site |
| Connect-screen failure router | `_handleConnectFailure :596-643`; `_needsKeyEnrolment :591`; top-notification-with-action pattern `:632-641` (0064 D7) | P2 |
| Keystore/key-error copy already written | `_friendlyError :519-545` | P1/P2 reuse |
| In-memory credential wipe on the client | `clearMemoryCredentials` (used at `connect_screen.dart:614`) | P2 action |
| Settings sections: `Storage :911`, `Connection :928`, Client identity tile `:975` | `settings_screen.dart` | P3 row placement |
| Settings token editor writes a hand-entered token | `settings_screen.dart:310` (`store.setToken(result)`) | marker set-site (via `setToken`), MADR F1b |
| Connect/settings widget-test harnesses with `FakeSettingsStore` / `_FakeStore` | `connect_screen_test.dart`, `settings_screen_test.dart` | P1–P3 tests |
| Settings "Re-pair this host" tile: clears pin + identity, keeps host + token; routed from nowhere | `settings_screen.dart:978-988`, `_repairHost :464-493` | P2 (converge on `clearSecrets`) |
| Cert-pin tile pattern: fingerprint subtitle + long-press copy | `settings_screen.dart:960-971` | P3 (D9 clones it) |
| Phone-side SPKI fingerprint helpers | `client_identity.dart:86-94` (`spkiFingerprintOfKeyPem`), `:98-105` (`debugSpkiFingerprint`) | P3 (D9) |
| Host: key checks pass/fail silently — `verifyClientKey` has the device + both fingerprints in scope; `writeAuthError` logs only unmapped errors | `internal/ws/server.go:968-979`, `:1656-1679` | P5 (D8 log line) |
| Host: `pair list` prints ID/NAME/CREATED/LAST_USED via tabwriter; store rows carry `ClientKeyFP` | `internal/cli/pair.go` (list `RunE`), `internal/auth/store.go:87` | P5 (D8 KEY column) |

### 0.2 Plugin behaviour the design leans on (pub cache, 10.3.1)

- Read/write failure with `resetOnError: true` → **per-key delete +
  retry**; Dart sees `null`/success, no exception
  (`FlutterSecureStorage.java:58-67`, `:110-120`, `:1308`). So *silent
  loss* is the common failure shape — detection must be inference (marker
  vs. absent secret), not exception handling.
- Cipher-mismatch init failures self-heal by wiping
  (`handleKeyMismatch :911`); remaining init failures throw on **every**
  operation — the `storageBroken` shape. `deleteAll()` itself needs no
  cipher (`:140-144`), so a Dart-side `deleteAll` is a plausible (not
  guaranteed) self-heal for that shape.

### 0.3 Notable non-facts (checked, not assumed)

- No published plugin version has a DataStore backend (MADR round-1
  correction) — nothing to migrate to; no pubspec change in this plan.
- The desktop plaintext fallback path is unaffected: marker and probe
  logic live above `_readSecret`/`_writeSecret`, which already fork per
  platform.

---

## P0 — SettingsStore: marker, scoped clear, probe, persisted diagnostics

All in `apps/mobile/lib/data/local/settings_store.dart` +
`test/settings_store_test.dart`.

1. **New pref keys** (non-secret, SharedPreferences):
   - `_kExpectCredentials = 'expect_credentials'` — "a token was saved and
     never deliberately cleared".
   - `_kLastStorageFailure = 'last_storage_failure'` — JSON
     `{op, error, at}` (ISO-8601 from the injected `_clock`).
2. **Marker lifecycle**: `setToken` sets `_kExpectCredentials = true`
   *after* `_writeSecret` succeeds; `clearToken` removes it. Nothing else
   touches it — the point is that a plugin-side silent wipe (0.2) cannot.
3. **`clearSecrets()`** (new): the `clearAll` secret loop
   (`clearToken`/`clearFingerprint`/`clearClientIdentity`, same
   attempt-all-then-rethrow semantics, `:817-832`) plus canary and marker
   removal — and **nothing from SharedPreferences** except the marker.
   Host, device id, relay route, transport stickiness, cwds, theme all
   survive. This is the recovery primitive for P1/P2.
4. **`clearAll` narrows** (MADR D3/F9): delete the three cwd removals
   (`:811-813`). Sign-out keeps path preferences; add the marker removal.
   The comment at `:814-816` already frames clearAll as
   credentials-focused — the code now matches it.
5. **`probeSecretStore()`** (new): returns
   `enum SecretStoreHealth { ok, credentialsLost, broken }`.

   ```text
   read token
     └─ throws → deleteAll() best-effort, retry read once
                   └─ still throws → record failure → broken
   write+delete canary '_kCanaryProbe' via _writeSecret/_clearSecret
     └─ throws → record failure → broken
   marker set && token == null → credentialsLost
   else → ok
   ```

   Notes: the `deleteAll` self-heal only fires when the store already
   throws (it is unusable — the wipe loses nothing recoverable, 0.2); the
   probe never *sets* the marker; `credentialsLost` stays true on every
   launch until re-pair writes a token or a deliberate clear removes the
   marker — no one-shot bookkeeping.
6. **Persist failures**: `_recordSecureFailure` additionally writes
   `_kLastStorageFailure` (fire-and-forget; prefs write failure is
   swallowed — diagnostics must never break the credential path). New
   `getLastStorageFailure()` returns the decoded record or null.

**Tests (extend `settings_store_test.dart`, existing fakes):**

- marker: set by `setToken`, cleared by `clearToken`/`clearSecrets`/
  `clearAll`, untouched by reads.
- `clearSecrets`: with `_InMemorySecureStorage` seeded with all four
  secrets + prefs seeded with host/cwds — exactly token, pins, client
  cert/key, canary removed; host and cwds intact.
- `clearAll`: cwd prefs now survive; host/deviceId/relay still cleared.
- probe state machine: ok (fresh store); ok (paired store);
  `credentialsLost` (marker true, `_InMemorySecureStorage` emptied behind
  the store's back); `broken` (`_FailingSecureStorage`); self-heal
  (`_ControlledSecureStorage` throwing until `deleteAll`, then ok) —
  probe returns `ok`/`credentialsLost` after healing, and the failure
  record is persisted.
- `getLastStorageFailure` round-trip with the injected clock.

## P1 — Cold-start detection and the one banner (connect screen)

`apps/mobile/lib/features/connect/connect_screen.dart` +
`connect_screen_test.dart`.

1. In `_loadSaved`, before the credential reads (`:442-444`):
   `final health = await store.probeSecretStore();`. On `credentialsLost`
   or `broken`, set new state `_storeHealth` and **skip auto-connect**
   (there is no token; today this path silently lands on an empty form —
   the incident's "acted like a fresh install" moment).
2. **Banner widget**: one persistent `Card` at the top of the form column
   (above the host field), error-styled, not a toast:
   - `credentialsLost`: *"This phone's stored credentials were reset —
     this can happen across app updates. Pair again with a new code; your
     hosts and preferences are intact."* + `FilledButton` **"Enter code"**
     → existing `_enterCode()`.
   - `broken`: the existing keystore copy from `_friendlyError`'s
     `SecureStorageUnavailable` branch (`:522-526`), no action button
     (restart/screen-lock is the only fix).
3. Banner clears reactively: pairing writes a token via `setToken`
   (marker restored) and `_goAfterConnect` navigates; no dismiss
   bookkeeping.

**Tests:** `FakeSettingsStore` gains `probeResult` (default `ok`) and
records `probeCalled`. Banner renders for `credentialsLost` with working
"Enter code" action (reuse the existing `_enterCode` sheet finders);
no banner on `ok`; auto-connect skipped when lost (fake client records no
`connect` call); `broken` renders the keystore copy.

## P2 — One-tap recovery on host key rejections

Same files as P1.

1. In `_handleConnectFailure`, alongside the existing `spentCode`
   top-notification pattern (`:626-642`): when `_needsKeyEnrolment(e)`
   (`client_key_required` / `client_key_mismatch`), show:
   `showTopNotification(context, 'This phone's key no longer matches the
   host. Reset and re-pair to fix it.', severity: error, actionLabel:
   'Reset & re-pair', onAction: ...)` where the action runs, in order:
   `store.clearSecrets()` → `client.clearMemoryCredentials()` →
   `_tokenCtrl.clear()` → `unawaited(_enterCode())` (mounted-guarded, the
   `:637-640` pattern). The long-form card copy (`:538-545`) stays.
2. No change to the `invalid_token` branch — it already clears the token
   with its own copy (`:612-618`), and `clearToken` clearing the marker
   keeps the P1 banner honest (a deliberate clear is not a "reset behind
   your back").
3. **Settings tile convergence (D4 round-2 amendment, F12):**
   `_repairHost` (`settings_screen.dart:464-493`) switches its body from
   `clearFingerprint()` + `clearClientIdentity()` to
   `store.clearSecrets()` + `client.clearMemoryCredentials()`, and its
   dialog copy widens from "host certificate changed" to also name the
   key-mismatch case ("…or this phone's key no longer matches the
   host"). The tile subtitle drops "keep host and token" (the token is
   dead weight in the mismatch case; the host is still kept).

**Tests:** drive `_handleConnectFailure` via the existing failure-path
harness with a `client_key_mismatch` `McException`: notification with
action appears; tapping it calls `clearSecrets` (fake store flag), clears
the client's in-memory credentials (fake client flag), and opens the
enter-code sheet — the assertion lands on the *pairing flow being on
screen*, not just on the handler running (the MADR's Tailscale
dead-button lesson). Negative: `invalid_token` still gets no reset chip.
Settings side: re-pair tile runs `clearSecrets` (not the two partial
clears) and navigates to `/`.

## P3 — Diagnostics: failure row + identity fingerprint (settings screen)

`apps/mobile/lib/features/settings/settings_screen.dart` +
`settings_screen_test.dart`.

1. In the **Storage** section (`:911`): a read-only `ListTile`
   "Secret storage" — subtitle `"No failures recorded"` or
   `"<op> failed <local time>: <error>"` from `getLastStorageFailure()`,
   loaded alongside the section's existing async state.
2. **D9 — Client identity tile** (`:973-977`): subtitle upgrades from
   `present`/`absent` to the SPKI fingerprint
   (`spkiFingerprintOfKeyPem` on the stored key PEM; keep `absent` when
   none), with the cert-pin tile's long-press-to-copy behaviour cloned
   from directly above (`:960-971`). Compute once in the screen's
   existing load path, not per build (the EC math in
   `client_identity.dart:86-94` is not free); `debugSpkiFingerprint`'s
   never-throw contract (`:98-105`) is the model — a parse failure
   renders `unreadable`, never an exception.

**Tests:** both failure-row subtitle states via `_FakeStore`; identity
tile shows a known fingerprint for a seeded identity, `absent` without
one, and copies on long-press (clipboard mock, as the pin-tile tests
do).

## P4 — Docs, gates, status flips

1. `ops-android-signing.md` §5: add the post-incident note — in-place
   updates are *expected* to preserve pairing from the 0066 build onward;
   if a store reset still occurs, the in-app banner + re-pair is the
   recovery (no data wipe); after any re-enrolment, prune the orphan
   device row (`mcremote pair list` / `pair revoke` / `pair prune`).
2. `ops-hardware-validation.md`: new Part E (Parts A–D are taken):
   - E1: upgrade current release → 0066 release over the top, still
     paired, sessions open (MADR U5).
   - E2: the *next* release after 0066 over the top — the incident's
     actual repro (MADR U6). If the store dies, E2 passes when the banner
     + one-tap re-pair recovers with preferences intact.
   - E3 (negative): Settings → clear token deliberately → relaunch → no
     "credentials were reset" banner.
   - Note recorded in the doc: the silent-wipe *detection* path cannot be
     simulated on unrooted hardware; it is covered by the P0/P1 tests.
3. MADR 0066 → Implemented (software rows) with E-rows tracked, and the
   0065 MADR/plan phone stages point at E1/E2 as their unblock condition
   (MADR D6).

## P5 — Host observability (D8; order-independent of P0–P4)

`internal/ws/server.go`, `internal/cli/pair.go`, existing test files
alongside each.

1. **Structured mismatch log**: in `verifyClientKey`
   (`server.go:968-979`) — the one place with the device *and* both
   fingerprints in scope — log at Warn on each failure before returning:

   ```go
   s.log.Warn("client key rejected",
       slog.String("device_id", dev.ID),
       slog.String("device_name", dev.Name),
       slog.String("reason", "mismatch"),        // or "missing"
       slog.String("enrolled_fp", fpPrefix(dev.ClientKeyFP)),
       slog.String("presented_fp", fpPrefix(c.clientKeyFP)),
   )
   ```

   `fpPrefix` truncates to 12 chars (`-` when empty) — enough to
   visually match against D9's tile and `pair list`, without pasting
   whole fingerprints into logs. `writeAuthError` (`:1656-1679`) is
   unchanged: it lacks device context and its peer-facing messages must
   stay fixed.
2. **`pair list` KEY column**: the list `RunE` (`internal/cli/pair.go`)
   adds `KEY` to the tabwriter header and prints the same 12-char prefix
   (`-` for keyless legacy rows) from the store's `ClientKeyFP`
   (`internal/auth/store.go:87`).

**Tests:** Go-side — `verifyClientKey` failure paths emit the Warn record
(capture with a `slog` test handler), success emits nothing; `pair list`
output includes KEY with a prefix for enrolled rows and `-` for keyless
rows, extending the existing CLI list tests. No protocol or message-shape
assertions change anywhere.

## File checklist

| File | Phases |
|---|---|
| `apps/mobile/lib/data/local/settings_store.dart` | P0 |
| `apps/mobile/test/settings_store_test.dart` | P0 |
| `apps/mobile/lib/features/connect/connect_screen.dart` | P1, P2 |
| `apps/mobile/test/connect_screen_test.dart` | P1, P2 |
| `apps/mobile/lib/features/settings/settings_screen.dart` | P3 |
| `apps/mobile/test/settings_screen_test.dart` | P3 |
| `docs/ops-android-signing.md` | P4 |
| `docs/ops-hardware-validation.md` | P4 |
| `docs/0066-MADR-secure-storage-upgrade-resilience.md` | P4 (status) |
| `docs/0065-MADR-update-automation.md`, `docs/0065-PLAN-update-automation.md` | P4 (gate cross-ref) |
| `internal/ws/server.go` + server tests | P5 |
| `internal/cli/pair.go` + CLI tests | P5 |

No pubspec, Android-manifest, Gradle, protocol, or CI changes anywhere;
daemon changes are exactly the P5 log line and CLI column.

## Verification map (MADR → plan)

| MADR | Plan | Kind |
|---|---|---|
| U1 probe state machine | P0 tests | unit |
| U2 clearSecrets scope / clearAll narrowing | P0 tests | unit |
| U3 banner renders + routes to pairing | P1 tests | widget |
| U4 reset-and-re-pair chip | P2 tests | widget |
| U5 upgrade happy path | P4 row E1 | hardware |
| U6 repeated-upgrade repro | P4 row E2 | hardware |
| U7 storage-broken fallback | P0 (`_FailingSecureStorage`) + P1 broken-banner test | unit + widget |
| U8 host mismatch log + KEY column | P5 tests | go unit |
| U9 fingerprint tile + converged re-pair tile | P3 + P2 tests | widget |
| — deliberate clear ≠ lost banner | P4 row E3 + P0 marker tests | unit + hardware |

## Edge cases held by design (for review)

- **Typed host, never paired**: marker never set → no banner (the marker,
  not host presence, gates `credentialsLost`).
- **`invalid_token` auto-clear** (`connect_screen.dart:612-618`): goes
  through `clearToken` → marker cleared → no false banner; that flow keeps
  its own copy.
- **Process death between `_writeSecret(token)` and the marker write**:
  token present, marker absent → probe reads `ok` (the condition is
  `marker && token == null`); the next `setToken` repairs the marker.
- **Desktop/iOS**: probe runs everywhere; on desktop the plaintext
  fallback makes `broken` effectively unreachable (by design); the banner
  copy avoids Android-specific claims.
- **MADR Q1** (canary verifies identity parses): deferred — the probe
  checks the store, not each secret's content; a corrupt identity still
  surfaces as `client_key_mismatch` and lands on the P2 chip.
- **MADR Q2** (flags on other platforms): kept cross-platform, per above.

## Sequencing and commits

One commit per phase (P0 → P5), `git commit --no-edit`; before each
commit, `flutter analyze` + `flutter test` green, and for P5 the Go suite
(`go test ./...`) green; no pushes. P5 touches only the daemon/CLI and can
land at any point; the phone phases land in order. On approval the phases
are independent enough that review is possible after any of them.
