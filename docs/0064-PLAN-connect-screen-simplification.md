# MADR 0064 — Implementation plan: connect screen simplification

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Proposed — for review. Not implemented.
- **Date**: 2026-08-02
- **Source**: [0064-MADR-connect-screen-simplification.md](0064-MADR-connect-screen-simplification.md)
  (all review questions closed; D1, D4+D4a, D5, D6, D7; D3 withdrawn; D2
  reduced on round 3 to a record of existing behaviour — the dynamic
  "Claim & connect" label **stays**)
- **Related**: 0062 (transport selection, deferral D5, amendment A1),
  0063 (liveness), 0046 (pair hints M-2)

Every claim below is grounded against the working tree at the time of writing
(`connect_screen.dart` 1373 lines, `settings_screen.dart` 937 lines,
`settings_store.dart`, `mcremote_client.dart`, `top_notification.dart`,
`app.dart`). Line numbers are anchors, not contracts — they will drift as the
phases land.

---

## 0. Assessment vs codebase (grounding)

### 0.1 What already exists (reuse, do not rebuild)

| MADR needs | Already in the tree |
|---|---|
| A collapsed-disclosure idiom for D1 | `ExpansionTile` used by the Advanced token tile, `connect_screen.dart:1215` |
| A Connect that claims a held code (D2) | `_onConnectPressed` → `_claimCode(_pendingPairCode)` else `_connect()`, `connect_screen.dart:939-946` |
| Mesh default with no gate (D2) | `_effectiveTransport` → `_selection ?? TransportMode.mesh` when both available, `connect_screen.dart:243-258` |
| `/settings` routable while unpaired (D4a) | `app.dart:43-46` route; redirect at `app.dart:48-69` bounces only `/sessions*` |
| The push-navigation idiom for Settings | `context.push('/settings')` from sessions, `sessions_screen.dart:1262` |
| Obscured token field with show/hide (D4) | `connect_screen.dart:1222-1243` — moves, not rewritten |
| The deferral Auto must skip (D6) | `if (_availability.bothAvailable)` early-return in `_applyPair`, `connect_screen.dart:666-682`; held code in `_pendingPairCode` |
| Mesh→relay fallback pre-claim (D6/V6) | DialEpisode, forced primary keeps its fallback for user-initiated dials (0062 A5); already tested in `dial_episode_test.dart` |
| Spent-code detection (D7) | `_EpisodeCtx.credentialSpent` latched at the point of no return, `mcremote_client.dart:1480`; surfaced as `lastDialSpentCredential`, `:1172`; consumed by `_handleConnectFailure`, `connect_screen.dart:598` |
| Top notification with severity + action (D7) | `showTopNotification`, `top_notification.dart:80`; error severity gets a rail + icon; an action extends display to 6 s (`_kActionDuration`) |
| Prefs idiom for `connect_mode` (D6) | Plain-SharedPreferences getters with validation-and-default, e.g. `getDefaultThinkingLevel`, `settings_store.dart:390-409` |
| Radio-dialog picker idiom for Settings | `_pickDefaultThinkingLevel`, `settings_screen.dart:549-576` |

### 0.2 Gaps (not present today)

1. **No `connect_mode` preference** — no store key, no getter/setter, no UI.
2. **The deferral is unconditional** — `_applyPair` defers *every*
   dual-available payload, including token QRs (`_pendingPairCode` is set to
   `null` for tokens but the early return still stops the dial,
   `connect_screen.dart:666-682`). D6 scopes the pause to `payload.hasCode`
   **and** Select mode.
3. **No Settings entry from the connect screen** — its AppBar menu has exactly
   one item, "Clear saved credentials" (`connect_screen.dart:1101-1111`).
4. **No token entry in Settings** — the token field lives only in the connect
   screen's Advanced tile.
5. **Spent-code failure is card-only** — `_handleConnectFailure` appends to the
   status card (`connect_screen.dart:599-607`); no top notification, no
   recovery action, no client log line.

(D2 contributes no gap: its facts — Connect claims a held code, no transport
gate, mesh default — are already true, and its label change was withdrawn. The
conditional `'Claim & connect'` label at `connect_screen.dart:1272-1276` stays
exactly as it is.)

### 0.3 Findings that shape the plan (checked, not assumed)

#### F1 — The Auto default flips the behaviour existing tests assert (BLOCKING for P0 ordering)

Three widget tests pin today's always-defer behaviour:
`'a dual-available QR *code* is not auto-claimed'`
(`connect_screen_test.dart:540`), `'a deferred QR code is claimed by Connect,
not lost'` (`:562`), and `'both transports up: the menu appears and nothing is
dialled'` (`:467`). Once the default is `auto`, the first two describe
**Select mode only** and must explicitly set it; the third still holds for a
token-less probe pass but must be re-checked against the token-QR exemption.
The phase that flips behaviour (P3) updates these tests in the same commit —
never in a later one.

#### F2 — `FakeSettingsStore` will hit real SharedPreferences unless extended

`FakeSettingsStore extends SettingsStore` and overrides only the secure-store
methods (`connect_screen_test.dart:13-73`). The connect screen currently reads
no plain preference, so nothing hits `SharedPreferences.getInstance()` in
widget tests. The moment `_applyPair` reads `getConnectMode()`, every existing
connect-screen test dials the real (unmocked) platform channel. Fix in the
same commit that adds the read: give `FakeSettingsStore` a
`String connectMode` field + override (default `'auto'`, matching production).

#### F3 — A pasted bare code has never deferred, and stays that way

`_pastePairUri` routes a bare `looksLikePairCode` string straight to
`_claimCode` (`connect_screen.dart:759-761`) — no probe pass, no deferral,
even today, even when both transports are up. This is existing behaviour the
MADR does not change, and it is defensible: a hand-pasted code has no relay
tuple of its own and the user is already mid-flow. **Recorded here so the D6
branch is not "helpfully" extended to it.** Scope of the D6 branch: the
`_applyPair` path only (QR scans and parsed pair URIs).

#### F4 — Moving the token to Settings needs a staleness seam

With go_router, `context.push('/settings')` keeps ConnectScreen mounted
underneath. A token saved in Settings therefore does **not** appear in the
already-loaded `_tokenCtrl`, and Connect would report "Host and token
required" seconds after the user typed a token one screen away. The AppBar
action must `await context.push('/settings')` and then re-read the stored
token. Rule to avoid clobbering in the other direction: **token always
refreshes from the store on return** (Settings is its only editor once D4
lands); **the Host field is only refreshed if currently empty** (a hand-typed
host must survive a Settings round-trip).

#### F5 — The D7 log line has exactly one correct emission point

`_finishFailedEpisode` (`mcremote_client.dart:1125-1140`) runs exactly once
per failed episode, already receives `spentCredential`, and is downstream of
the A1 latch — so V14's "exactly once per burn" holds by construction if the
log is emitted there. It does not currently know the transport or code; the
episode context does (`_EpisodeCtx`, the claim leg holds the normalized code
at `:1476`). Plumb both through the ctx rather than logging in the leg, where
a future second leg could double-emit.

#### F6 — Ordering: Settings must gain the token entry *before* the connect screen loses it

Each phase is a standalone commit that leaves the app whole. If P-connect
removed the Advanced tile before Settings could accept a token, that commit
would strand token users. So Settings lands first (P1), the connect screen
restructure second (P2). One commit of harmless duplication (token editable in
two places) is the cost.

#### F7 — Two copy fixes ride along

- The status-card spent-code text is hedged ("may have been used",
  `connect_screen.dart:604`) — the MADR's copy rules call that out as the bad
  pattern. The card keeps the long form (host command included) but goes
  definite.
- The MADR's V8 row said "defaults to `select`", contradicting closed decision
  4; fixed to `auto` alongside this plan.

Confirmed-good assumptions: `showTopNotification` renders over the **root**
overlay and is findable from widget tests (`top_notification_test.dart`
exercises exactly this); the Enter-code sheet's own button keeps its
"Claim & connect" label (it *does* claim immediately — D2's "always Connect"
rule is about the main button); go_router's redirect never bounces
`/settings` for an unpaired client.

### 0.4 Out of scope (per the MADR)

- Host field removal (D3 withdrawn) — revisit only if V1 fails.
- Any client/protocol/daemon/transport-policy change beyond the D7 log line.
- The pasted-bare-code path (F3).
- Preventing the burn itself — MADR: "No client change can prevent this."

---

## 1. Target design

### 1.1 New store API (`settings_store.dart`)

```dart
static const _kConnectMode = 'connect_mode';

/// Connect mode (MADR 0064 D6): what happens when a dual-available pair
/// *code* arrives. 'auto' claims immediately over mesh; 'select' pauses for
/// a transport choice. Tokens never pause in either mode.
Future<String> getConnectMode() async {
  final v = (await _p).getString(_kConnectMode);
  // Tolerate a corrupted value without inventing a third behaviour.
  return (v == 'select' || v == 'auto') ? v! : 'auto';
}

Future<void> setConnectMode(String mode) async {
  if (mode != 'select' && mode != 'auto') return;
  await (await _p).setString(_kConnectMode, mode);
}
```

Strings, not an enum: matches the file's own idiom (`theme_mode`,
`default_thinking_level`), and only two call sites read it.

### 1.2 Connect screen deltas (`connect_screen.dart`)

| Decision | Change |
|---|---|
| D1 | Headline `Text` + two-line instructions (`:1133-1144`) become one `ExpansionTile('Connect to your machine - Steps')`, `initiallyExpanded: false`, **no** state variable (never auto-opens). Body: "On the host running mcremote, run:" + `mcremote pair code --name <name of device>` (mono) + "to generate the QR and short-term code." |
| D2 | **No change.** `_onConnectPressed` and the dynamic label (`'Claim & connect'` while a code is held, `:1272-1276`) stay as-is; D2 is invariants-only after round 3. |
| D4 | Delete the Advanced `ExpansionTile` (`:1215-1245`) and the `_advanced`/`_showToken` fields. `_tokenCtrl` **stays** as invisible state: loaded in `_load`, written by token QRs (`:647-649`), by paste (`:750-757`, minus the `_advanced = true` line), by claim success (`:886`), cleared on invalid token. |
| D4a | AppBar menu gains `'Settings'` above `'Clear saved credentials'` → `await context.push('/settings')`, then the F4 refresh: token from store always, host only if the field is empty, then `unawaited(_refreshProbes())`. |
| D6 | See §1.4. |
| D7 | See §1.5. |

Resulting order (MADR "Resulting screen"): logo → Steps tile → Scan/Enter →
Paste → Host → transport block → relay display → Test healthz + Connect →
status card.

### 1.3 Settings deltas (`settings_screen.dart`)

Both in the existing **Connection** section (header at `:840`), after
`_buildRouteSection`:

- **Connect mode** — `ListTile(Icons.bolt_outlined)`, subtitle
  `'Auto — scan and connect over mesh'` / `'Select — choose a transport
  first'`; tap opens a `SimpleDialog` (thinking-level idiom, `:549-576`) with
  the two options and one-line descriptions; persists via `setConnectMode`.
- **Long-lived token** — `ListTile(Icons.key_outlined)`, subtitle
  `'present'`/`'absent'` (from `getToken()`); tap opens a dialog holding the
  obscured `TextField` + show/hide toggle transplanted from the Advanced tile,
  prefilled from the store. **Save** with non-empty text → `setToken`; Save
  with empty text → `clearToken` (subtitle flips to absent). Helper line names
  the source: `mcremote pair create`.

### 1.4 The D6 branch (one branch, as decided)

In `_applyPair`, the deferral block (`:666-682`) becomes:

```dart
final mode = await ref.read(settingsStoreProvider).getConnectMode();
if (!mounted) return;
final defer =
    _availability.bothAvailable && payload.hasCode && mode == 'select';
if (defer) {
  // …existing block, minus the hasCode conditional (it is now guaranteed)…
  return;
}
```

Everything below the branch already does the right thing for every non-defer
case:

- **Auto + code + both up**: falls through; `sole` is null so the status reads
  "claiming…" without a transport name until the episode reports one — adjust
  the `via` computation to use `_effectiveTransport` (which resolves **mesh**
  when both are up) so the status says "claiming over mesh…".
- **Token QR, either mode**: falls through to `_connect()` — the D6 exemption,
  new behaviour in *both* modes (today a dual-available token QR pauses).
- **Sole-available / neither**: unchanged, both modes (MADR: "Neither mode
  changes anything when only one transport is available").

`_claimCode` then passes `transport: _effectiveTransport` (mesh) with the
episode's fallback intact — which is exactly V6's "mesh refuses socket →
relay, code intact", already the client's tested behaviour.

### 1.5 The D7 surface

In `_handleConnectFailure`, after the existing `setState` (so the card and the
notification carry the same fact), when `spentCode`:

```dart
showTopNotification(
  context,
  'That pair code has been used. Get a new one and try again.',
  severity: NoticeSeverity.error,
  actionLabel: 'Enter code',
  onAction: () => unawaited(_enterCode()),
);
```

- Card copy goes definite (F7): "That pair code has been used. Ask the host
  for a new code (mcremote pair code --name phone), then scan or enter it."
- `_enterCode` already calls `_supersedeAutoConnect()` first — no extra
  wiring.
- Try Mesh / Try Relay stays withheld: `_buildTryOther` already returns
  nothing when `_failureSpentCode` (`:384`) — V13 pins it.

Client log line (F5): extend `_EpisodeCtx` with the transport of the leg that
spent the credential and the (normalized) claim code; in
`_finishFailedEpisode`, when `spentCredential`:

```dart
debugPrint(
  'mcremote: pair code spent without token '
  '(transport=${ctx.spentOn?.name}, code=${ctx.claimCode})',
);
```

The code is dead at this point (that is the premise of the message), so
logging it costs nothing and buys host-side correlation with
`mcremote pair list`.

---

## 2. Phased delivery

Each phase compiles, passes `flutter analyze` + `flutter test` (and
`dart format`), and is committed with `--no-edit` before the next begins.
Nothing is pushed until all phases are complete.

### P0 — Store plumbing (D6 storage)

- `settings_store.dart`: `_kConnectMode`, `getConnectMode`, `setConnectMode`
  (§1.1).
- `settings_store_test.dart`: default is `auto`; `select` persists; corrupt
  value (`'sometimes'`) reads back as `auto` (**V8**).
- No behaviour change anywhere — invisible commit.

### P1 — Settings gains both entries (D4 destination, D6 control)

Lands **before** the connect screen loses anything (F6).

- `settings_screen.dart`: Connect mode tile + dialog; Long-lived token tile +
  dialog (§1.3). State: `String _connectMode = 'auto'` and
  `bool _tokenPresent`, both loaded in `_loadConnectionInfo`.
- `settings_screen_test.dart`: the tile shows the stored mode and the dialog
  persists a change; the token dialog saves to the store and the subtitle
  flips present/absent; empty save clears. (Extend the file's existing fake
  store with `connectMode`/token fields as needed.)
- Connect screen untouched — the token is briefly editable in two places.

### P2 — Connect screen restructure (D1, D4a, D4 removal)

- Steps `ExpansionTile`, always-collapsed (D1); Advanced tile deleted, token
  becomes invisible state (D4); AppBar Settings action + F4 refresh-on-return
  (D4a); `_advanced = true` dropped from the paste path. The main button is
  **not touched** — its dynamic label stays (D2, round 3).
- Rider (owner request, 2026-08-02): the gap below the logo shrinks 24 → 20
  (`SizedBox(height: 24)`, `:1132`), matching the screen's 20 px rhythm.
  Nothing aligns against it; everything below shifts up 4 px, in V1's favour.
- Tests, same commit:
  - **V1**: at 360×640 logical (dpr 1), Connect's rect lies fully inside the
    viewport with no scrolling. If this fails legitimately, the fix is layout
    trimming (padding), not test loosening — and if it cannot be made to pass,
    stop and surface it: the MADR says a V1 failure reopens D3.
  - **V9**: AppBar menu → Settings pushes the Settings screen while the fake
    client is unpaired/disconnected.
  - **V2** needs no new test: `'a deferred QR code is claimed by Connect'`
    (`:562`) already pins both the claim routing and the `'Claim & connect'`
    label, and stays valid unchanged.
  - `'renders the core pairing affordances'` (`:241`) re-pinned to the new
    structure (Steps tile present + collapsed; no Advanced tile; token field
    absent).
- **Deferral behaviour untouched in this phase** — `_applyPair` still defers
  unconditionally, so the F1 tests still pass unmodified here.

### P3 — The D6 branch (behaviour flip)

- `_applyPair` branch per §1.4; status copy for the auto path
  ("claiming over mesh…").
- `FakeSettingsStore.connectMode` override added (F2), default `'auto'`.
- Tests, same commit (F1):
  - **V4**: `connectMode: 'select'` + dual-available code QR → `claimCalls`
    stays 0 until Connect. (Re-scope of `:540` and `:467`.)
  - **V5**: `connectMode: 'auto'` + the same QR → `claimCalls == 1`
    immediately, `lastTransport == TransportMode.mesh`.
  - **V7**: dual-available **token** QR → `connectCalls == 1` immediately in
    *both* modes; never a "choose one" status.
  - **V3** (already largely covered at `:493`): re-affirm Connect dials mesh
    with both up and no selection.
  - **V6**: client-level — confirm `dial_episode_test.dart` covers a forced-
    mesh primary whose socket is refused falling back to relay with
    `credentialSpent == false`; add the case if the existing matrix lacks the
    forced-primary variant.

### P4 — D7: notification, copy, log line

- `_handleConnectFailure` notification + definite card copy (§1.5).
- `mcremote_client.dart`: `_EpisodeCtx.spentOn`/`claimCode`, log in
  `_finishFailedEpisode`.
- Tests, same commit:
  - **V12**: `claimError` + `spentCredential: true` on the fake client → the
    notification text and the `'Enter code'` action appear (root overlay;
    follow `top_notification_test.dart`'s finding pattern); tapping the action
    opens the Enter-code sheet.
  - **V13**: same failure → no `'Try Mesh'` / `'Try Relay'` anywhere.
  - **V14** (unit, `dial_episode_test.dart` or `mcremote_client_test.dart`):
    capture `debugPrint`; one burnt episode emits the line exactly once; a
    non-spent failure emits it never.
- A spent-code failure whose ConnectScreen has been disposed (auto-connect
  redirect race) skips the notification via the existing `mounted` guard —
  acceptable: the card path already handles that window, and the client keeps
  `lastDialSpentCredential` for the next build.

### P5 — Docs close-out

- `ops-hardware-validation.md`: add rows **V10** (token entered in Settings
  pairs), **V11** (QR → transport → Connect end-to-end, both modes), **V15**
  (burn leaves an orphan in `mcremote pair list`; `pair prune` clears it) —
  macOS + Linux instructions per that doc's format, including how to stage a
  burn (kill mcremote between socket-open and `pair_ok`, or drop the network
  at the claim moment as done for 0062 G7).
- MADR 0064 status → Implemented (with the D7 host-side orphan note
  cross-linked from the validation doc).

---

## 3. File-level checklist

| File | P0 | P1 | P2 | P3 | P4 |
|---|---|---|---|---|---|
| `lib/data/local/settings_store.dart` | ● | | | | |
| `lib/features/settings/settings_screen.dart` | | ● | | | |
| `lib/features/connect/connect_screen.dart` | | | ● | ● | ● |
| `lib/data/ws/mcremote_client.dart` | | | | | ● |
| `test/settings_store_test.dart` | ● | | | | |
| `test/settings_screen_test.dart` | | ● | | | |
| `test/connect_screen_test.dart` | | | ● | ● | ● |
| `test/dial_episode_test.dart` (or client test) | | | | ● | ● |
| `docs/ops-hardware-validation.md` + MADR status | | | | | P5 |

Not touched: `transport_policy.dart`, `transport_probes.dart`, `pair_uri.dart`,
anything in `internal/` (Go), `app.dart` (the route already exists).

## 4. Verification-to-phase map

| MADR check | Phase | Level |
|---|---|---|
| V1 fold test | P2 | widget |
| V2 claim-vs-token Connect + dynamic label | existing `:562` flow test, unchanged | widget |
| V3 no gate, mesh default | P3 (re-affirmed; exists at `:493`) | widget |
| V4 Select defers | P3 | widget |
| V5 Auto claims over mesh | P3 | widget |
| V6 pre-claim fallback, code intact | P3 | client fake |
| V7 token never pauses | P3 | widget |
| V8 default `auto`, persists | P0 | unit |
| V9 Settings while unpaired | P2 | widget |
| V10 token via Settings pairs | P5 | hardware |
| V11 end-to-end, both modes | P5 | hardware |
| V12 burn notification + action | P4 | widget |
| V13 no Try-other on burn | P4 | widget |
| V14 log line once per burn | P4 | unit |
| V15 orphan visible, prunable | P5 | hardware |

## 5. Remaining implementation choices (defaults chosen, flag to change)

1. **Settings token dialog prefills the real token (obscured)** — parity with
   today's Advanced field, which also holds it. Alternative (never prefill,
   write-only) is more paranoid but breaks the show/hide affordance the MADR
   explicitly keeps.
2. **Empty token + Save = clear** — the only way to remove a mistyped token
   without "Clear saved credentials" nuking the host too.
3. **Enter-code sheet button keeps "Claim & connect"** — it is not the main
   button, and it genuinely claims on tap.
4. **Notification copy and card copy differ deliberately** — short + action at
   the top, long + host command in the card, per D7.
