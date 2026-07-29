# MADR 0052 — Implementation plan: thinking levels and the settings panel

<!-- markdownlint-disable MD013 MD060 -->

Companion to [MADR 0052](./0052-MADR-thinking-levels-and-settings.md). Read that
first — especially §2 (what each provider actually exposes), §6 (the six
settings features) and §8 (the resolved decisions D5–D8).

- **Status**: Proposed — for review
- **Date**: 2026-07-29
- **Line references**: verified at `88ae03f`
- **Measured against**: codex 0.145.0, grok 0.2.114, opencode 1.18.7, goose 1.44.0

---

## 0. Summary

Two independent tracks. **Track A** is thinking levels (daemon + protocol +
client). **Track B** is six settings-screen features that share a screen but
nothing else. They can ship in either order; only Phase A6 and Phase B1 touch
the same file, and they touch different sections of it.

| Track | Phase | Layer | Change |
|---|---|---|---|
| A | A1 | `internal/picker` | `ThinkingLevel` type, `Option.ThinkingLevels`, ordering |
| A | A2 | `internal/provider/codex` | parse `supportedReasoningEfforts`; `effort` on `turn/start` |
| A | A3 | `internal/provider/acpagent` + `grok` | parse `_meta.reasoningEfforts`; per-session `--reasoning-effort` |
| A | A4 | `internal/session`, `internal/ws` | per-session thinking level; `/thinking` command |
| A | A5 | `apps/mobile` (data) | `ThinkingLevel` model, client plumbing |
| A | A6 | `apps/mobile` (UI) | level chips in the model picker; chat chip |
| B | B1 | `apps/mobile` | remove `preferredProvider()` + provider-scoped default model |
| B | B2 | `apps/mobile` | default session mode (§6.1) |
| B | B3 | `apps/mobile` | notification granularity (§6.2) |
| B | B4 | `apps/mobile` | transcript storage: size + clear (§6.3) |
| B | B5 | `apps/mobile` | pinned working directories (§6.4) |
| B | B6 | `apps/mobile` | Enter-key behaviour in the composer (§6.5) |
| B | B7 | `apps/mobile` | connection & security card (§6.6) |
| — | C1 | — | live verification across all four binaries |

**Dependency order.** A1 → A2/A3 (parallel) → A4 → A5 → A6. B1 first in track B
(it deletes code the others would otherwise have to keep working), then
B2–B7 in any order. C1 last. B6 and B7 touch no daemon code and no file any
other phase edits, so they can be picked up first if a quick win is wanted.

**Commit protocol.** One commit per phase. `make pre-add-check FILES="…"` before
every `git add` of a Go file; `dart format` + `flutter analyze` before every Dart
commit. Commit with `GIT_EDITOR=true git commit` — never `-m`/`-F`. Do not push
unless asked.

---

## Track A — thinking levels

### Phase A1 — picker type and ordering

**File:** `internal/picker/option.go` (type), `internal/picker/order.go` (docs)

1. Add the type from MADR D5 verbatim, in `option.go` beside `Option`.
2. Add `ThinkingLevels []ThinkingLevel \`json:"thinking_levels,omitempty"\`` to
   `Option`.
3. Do **not** add `Meta` keys — D5 rejected the packed-string form. Leave
   `order.go`'s `Meta*` constants alone; add a doc line pointing at the new
   field so the next reader does not add a fourth spelling.
4. Helper, since two providers need the same normalisation:

   ```go
   // NormalizeThinkingLevels returns levels cheapest-first and guarantees at
   // most one Default. Providers disagree on order — codex returns
   // cheapest-first, grok returns high,medium,low — and the client renders a
   // slider, so the daemon fixes the direction once here rather than in each
   // dialect (MADR 0052 D2).
   //
   // Order is taken from the provider's own list; `known` supplies the rank of
   // the names every vocabulary shares. An unrecognised rung keeps its relative
   // position rather than being dropped: codex types the value as an open
   // string, so a new name is expected, not exceptional.
   func NormalizeThinkingLevels(in []ThinkingLevel) []ThinkingLevel
   ```

   Rank table: `none < minimal < low < medium < high < xhigh < max < ultra`.
   Unknown names sort after known ones, preserving input order (stable sort).

**Tests** (`internal/picker/thinking_test.go`, new):

- grok's real payload order (`high, medium, low`) normalises to
  `low, medium, high`;
- codex's real order (`low…ultra`) is unchanged;
- a list containing an unknown rung keeps it, in relative position;
- exactly one `Default` survives when a provider marks several.

**Acceptance:** `go test ./internal/picker/`.

### Phase A2 — codex: parse the list, send the effort

**File:** `internal/provider/codex/provider.go`, `session.go`

#### A2.1 Parse what is already on the wire

`modelListEntry` (`provider.go:85-96`) parses `defaultReasoningEffort` and
ignores `supportedReasoningEfforts`. Both are **required** fields in codex's own
schema. Add:

```go
SupportedReasoningEfforts []struct {
    ReasoningEffort string `json:"reasoningEffort"`
    Description     string `json:"description"`
} `json:"supportedReasoningEfforts"`
```

and in `listModelsVia` (`provider.go:127+`) build `opt.ThinkingLevels`, marking
`Default` where `ReasoningEffort == m.DefaultReasoningEffort`. Keep the existing
`meta["reasoning_effort"]` write — it is the badge the picker already renders,
and removing it is a separate cosmetic decision.

Live shape to code against (measured, `codex 0.145.0`):

```text
gpt-5.6-terra  default=medium  low medium high xhigh max ultra
gpt-5.4-mini   default=medium  low medium high xhigh
```

#### A2.2 Send it on `turn/start`

The session already builds `turn/start` params. Add `effort` when the session
has a thinking level set, and **only then** — omitting the key inherits codex's
own default, which is what "Provider default" must mean.

Per D7 this is next-turn by construction: a level set mid-turn lands on the
following `turn/start`. No special handling needed; the UI wording carries it.

**Tests** (`internal/provider/codex/thinking_test.go`, new):

- a `model/list` fixture built from the captured payload yields six levels for
  `terra`, four for `5.4-mini`, with the right `Default`;
- `turn/start` carries `effort` when set and **omits the key entirely** when
  not — assert absence, not empty-string;
- a model whose `supportedReasoningEfforts` is `[]` produces no levels.

Verify the omission test bites by hard-coding `effort: ""` and watching it fail.

### Phase A3 — grok: parse the list, spawn with the flag

**Files:** `internal/provider/acpagent/acpagent.go`, `config.go`,
`internal/provider/grok/grok.go`

#### A3.1 Parse `reasoningEfforts`

`GrokAvailableModel.Meta` (`acpagent.go:762-768`) has `supportsReasoningEffort`
and `reasoningEffort` but not the list. Add:

```go
ReasoningEfforts []struct {
    ID          string `json:"id"`
    Value       string `json:"value"`
    Label       string `json:"label"`
    Description string `json:"description"`
    Default     bool   `json:"default"`
} `json:"reasoningEfforts"`
```

`modelsToCatalog` (`acpagent.go:779-800`) currently maps only id/name/group —
populate `ThinkingLevels` from the above, preferring `Value` over `ID` for the
wire value (they are equal in the measured payload; `Value` is what the flag
takes). Gate on `supportsReasoningEffort`: if false, emit no levels even if a
list is present.

Measured payload for `grok-4.5`: `high` (default), `medium`, `low`, each with
`label` and `description`.

#### A3.2 Per-session `--reasoning-effort`

`acpagent` spawns one process per session, so the level is a spawn argument.
Today `grok.go:120` appends the flag from daemon config only.

- `acpagent.Config` gains nothing; instead `provider.StartOptions` gains
  `ThinkingLevel string`, threaded to `defaultArgs`.
- Precedence: per-session value → `providers.grok.reasoning_effort` config →
  omit the flag.
- **Keep the argv position rule from MADR 0050 D1**: `--reasoning-effort` is a
  *global* flag and must stay before the `agent` subcommand. Putting it after
  makes grok refuse to start.

#### A3.3 Mid-session is not supported — say so

`SetThinkingLevel` on an ACP session returns a typed error
(`ErrThinkingLevelFixed`) that the command layer renders as "grok applies
thinking level at session start; this takes effect for new sessions". Do **not**
attempt `session/set_model` with an extra field: measured, grok returns
`{"model":{"Ok":"grok-4.5"}}` for both a bare and a `_meta`-wrapped variant, so
success is indistinguishable from silent ignore (MADR §2.2).

**Tests:** argv assertion that the flag precedes `agent` and reflects the
per-session value (extend `grok/live_argv_test.go`'s pinning style); a
`modelsToCatalog` unit test from the captured payload; `SetThinkingLevel`
returns the typed error.

### Phase A4 — daemon: per-session level and the `/thinking` command

**Files:** `internal/provider/provider.go`, `internal/session/`,
`internal/command/`, `docs/protocol-v1.md`

1. Capability interface, mirroring `ModelSession`:

   ```go
   // ThinkingSession is implemented by sessions that accept a thinking level.
   // Absence is the honest answer for opencode and goose (MADR 0052 D6).
   type ThinkingSession interface {
       SetThinkingLevel(ctx context.Context, level string) error
       ThinkingLevel() string
   }
   ```

2. `session.Manager`: store the level on the entry; report the op in
   `commands.go` capability probing beside `OpSetModel`
   (`internal/session/commands.go:62`).
3. `session.create` accepts `thinking_level`; forwarded to `StartOptions`.
4. Canonical command `/thinking` in the MADR 0023 registry — advertised only
   when the session implements the interface, so it never appears for opencode
   or goose. `cmdThinking` mirrors `cmdModel`.
5. `docs/protocol-v1.md`: document `thinking_levels` on picker options, the
   `thinking_level` create-session field, and `/thinking`.

**Tests:** a fake session implementing `ThinkingSession` proves the op appears;
one that does not proves `/thinking` is absent from the advertised list.

### Phase A5 — mobile data layer

**Files:** `apps/mobile/lib/data/protocol/picker.dart`, `models.dart`,
`data/ws/mcremote_client.dart`, `data/local/settings_store.dart`

1. `ThinkingLevel` model with tolerant parsing (match `PlanEntry.fromJson`
   style); `PickerOption.thinkingLevels`.
2. `createSession(..., String? thinkingLevel)`; `setThinkingLevel(sessionId,
   level)`.
3. `SettingsStore`: `defaultThinkingLevel` — `null` (= provider default),
   `low`, `medium`, `high`. **Only those three names** — the resolution rule
   below can never select `xhigh`/`max`/`ultra` (MADR D3).
4. The resolver, in `data/` not in a widget, because both the create sheet and
   the `/model` flow need it:

   ```dart
   /// Resolve the user's global intent against one model's advertised ladder.
   ///
   /// Exact name match only, then the model's own default, then nothing.
   /// Deliberately NOT rank interpolation: on a six-rung model that would map
   /// "High" onto `ultra`, silently escalating cost (MADR 0052 D3).
   String? resolveThinkingLevel(List<ThinkingLevel> levels, String? intent);
   ```

**Tests** (`test/thinking_level_test.dart`, new) — this is the safety-critical
unit:

- intent `high` + codex 6-rung ladder → `high`, **never** `ultra` or `max`;
- intent `high` + a hypothetical ladder without `high` → the model's default;
- intent `null` → null (send nothing);
- empty ladder → null;
- every intent resolves to a member of the input list or null, property-style
  over all four measured ladders.

### Phase A6 — mobile UI

**Files:** `features/widgets/option_picker_sheet.dart`,
`features/sessions/sessions_screen.dart`, `features/chat/chat_screen.dart`

1. Model rows with `thinkingLevels` render a segmented chip row beneath, default
   preselected via `resolveThinkingLevel`. Rows without levels render exactly as
   today — no empty space, no disabled control.
2. Create-session sheet passes the chosen level to `createSession`.
3. Chat: a thinking chip beside the mode chip when the session supports it. For
   grok it is read-only with a "new sessions only" hint (A3.3).
4. Settings row "Default thinking level" (see B-track screen layout).

**Tests:** widget test that a levels-bearing option shows chips and a bare one
shows none; that the resolved default is preselected.

---

## Track B — settings panel

### Phase B1 — delete the hardcoded default provider

**Files:** `data/ws/mcremote_client.dart`, `features/settings/settings_screen.dart`,
`data/local/settings_store.dart`

1. Delete `preferredProvider()` (`mcremote_client.dart:1885-1898`) — the
   hardcoded `['grok','opencode','goose','fake']` list that omits codex.
2. Delete the "Default model" row and `_pickPreferredModelFlow`
   (`settings_screen.dart:91-139, 277+`).
3. Delete `getPreferredModel`/`setPreferredModel` and the model-provider pair
   (`settings_store.dart:223-260`). Per D8: **no migration**. Leave orphaned
   `preferred_model_*` keys in place; B4's clear action reaches them.
4. In the create-session sheet, seed the model picker from the provider the user
   just chose. If a per-provider memory is still wanted it belongs here, keyed
   by the actual selection — but that is a follow-up, not this phase.

**Acceptance:** `grep -rn "preferredProvider\|preferred_model" lib/` returns
nothing; a codex-only host reaches the model picker without touching grok.

### Phase B2 — default session mode

**Files:** `settings_store.dart`, `settings_screen.dart`,
`features/sessions/sessions_screen.dart`

1. `defaultSessionMode(provider)` / `setDefaultSessionMode(provider, modeId)` —
   per provider, since mode ids are provider-scoped.
2. Settings row per configured provider, populated from the provider's
   advertised modes (the same list the chat mode selector uses).
3. **Confirm dangerous once, at set time.** Reuse the existing dialog path
   (`chat_screen.dart:2678`, gated on `SessionMode.dangerous`, never on the id —
   see MADR 0044 D1). Setting a dangerous default asks once; sessions then start
   in it without re-asking, which is the whole point.
4. Create-session applies the default after modes arrive, then the mode chip
   reflects it.

**Tests:** a dangerous default requires confirmation and is not stored on
cancel; a non-dangerous one stores without a dialog; a stored mode no longer
advertised by the provider is ignored rather than sent.

### Phase B3 — notification granularity

**Files:** `data/notifications/agent_notifications.dart`,
`notification_coordinator.dart`, `settings_store.dart`, `settings_screen.dart`

Today `shouldNotify` (`agent_notifications.dart:91-96`) hard-codes three types
and **errors are never notified at all** — so this phase adds a capability, not
just a split.

1. `NotifyKinds { asks, turnComplete, errors }` with three stored booleans,
   defaulting asks + turnComplete on (today's behaviour) and errors **on**
   (new — a failed turn is exactly what a backgrounded user wants to know).
2. `shouldNotify` takes the kind set instead of deciding alone; keep the
   `watching` short-circuit ahead of it unchanged.
3. `_onEvent` (`notification_coordinator.dart:125-152`) gains an `error` case
   using the existing `_labelFor` + notification id derivation.
4. Three switches nested under the existing master `Agent alerts`, disabled when
   the master is off.

**Tests:** each kind independently suppressible; master off suppresses all;
`watching` still wins over every kind; an `error` event notifies when enabled
and not when disabled.

### Phase B4 — transcript storage: size and clear

**Files:** `data/chat/transcript_cache.dart`, `settings_screen.dart`

1. `TranscriptCache.usage()` → `(int sessions, int bytes)`, summing the
   `_entryPrefix` blobs listed in `_indexKey` (`transcript_cache.dart:19-20`).
   Run it off the main isolate — the same `compute` the encode path uses — since
   it touches up to `kTranscriptCacheMaxSessions` blobs of ~400 KB.
2. A "Cached transcripts" row: `N sessions · X MB`, with a clear action.
   `clear()` already exists (`transcript_cache.dart:208`) and is already
   serialised through the mutation queue.
3. The clear must **not** touch credentials — that is the whole reason this row
   exists rather than "clear app data". Sweep orphaned `preferred_model_*` keys
   here too (D8).
4. After clearing, in-memory transcripts stay; only the on-disk snapshot goes.
   State the reload behaviour in the confirm dialog.

**Tests:** usage counts only cache keys and ignores unrelated prefs; clear
empties the index and every entry blob; clear leaves host/token/pins intact —
assert explicitly, this is the risky one.

### Phase B5 — pinned working directories

**Files:** `data/local/settings_store.dart`, `settings_screen.dart`,
`features/sessions/sessions_screen.dart`

1. `_kPinnedCwds = 'pinned_session_cwds'` beside `_kRecentCwds`
   (`settings_store.dart:59-60`), with `getPinnedCwds`, `pinCwd`, `unpinCwd`,
   `reorderPinnedCwd`. Ordered, user-controlled, **uncapped** (a pin is
   deliberate; the five-slot cap is a recency budget, not a storage limit).
2. `addRecentCwd` (`:211-221`) skips a path already pinned, so pinning frees a
   recency slot instead of consuming one.
3. Create-session dropdown (`sessions_screen.dart:698-724`): pinned group first
   with a pin icon, then recents, deduped against pinned.
4. Settings "Working directories": reorderable list, unpin/remove, and a "pin
   current" affordance from the recents list. This is also the first place a
   stale recent can be deleted.

**Tests:** pinned survive `kMaxRecentCwds` overflow of recents; a pinned path
never appears twice in the dropdown; unpin returns it to recency, not oblivion;
reorder persists.

**Note for B1's `clearAll`:** it already removes `_kRecentCwds`
(`settings_store.dart:624`). Add `_kPinnedCwds` beside it — a sign-out that
leaves the previous user's directory shortlist behind is a small privacy leak
and an easy one to miss.

### Phase B6 — Enter-key behaviour in the composer

**Files:** `data/local/settings_store.dart`, `features/settings/settings_screen.dart`,
`features/chat/chat_screen.dart`

The composer is `minLines: 1, maxLines: 5` but pins
`textInputAction: TextInputAction.send` with `onSubmitted: (_) => _send()`
(`chat_screen.dart:2389-2399`), which overrides the newline action a multi-line
field would otherwise get. A newline is therefore unreachable from the soft
keyboard and the five-line growth is only ever reached by pasted text
(MADR §6.5).

1. `SettingsStore`: `_kSendWithEnter = 'send_with_enter'`, `getSendWithEnter()`
   defaulting **true** (today's behaviour — this must not change under anyone
   silently), and `setSendWithEnter(bool)`.
2. Expose it as a Riverpod provider read by the chat screen, so toggling it
   applies to an open chat without a route bounce.
3. In the composer:

   ```dart
   textInputAction: sendWithEnter
       ? TextInputAction.send
       : TextInputAction.newline,
   onSubmitted: sendWithEnter ? (_) => _send() : null,
   ```

   Both must switch together. Leaving `onSubmitted` wired under
   `TextInputAction.newline` would send *and* insert a newline on some IMEs.
4. No layout change is needed: the send affordance already exists as the filled
   `Icons.schedule_send` button beside the field, so Enter-as-newline strands
   nobody. Verify it stays enabled in the same states it does today (it is
   gated on `busy`/`offline`, not on the Enter mode).
5. Settings row under **Appearance**: `Send with Enter`, subtitle
   "Off: Enter starts a new line and the send button sends."

**Tests** (`test/composer_enter_test.dart`, new):

- default is send-with-Enter — assert the stored default explicitly, since a
  regression here changes behaviour for every existing install;
- with the setting off, the field reports `TextInputAction.newline` and
  `onSubmitted` is null;
- with it off, tapping the send button still sends (the button is the only
  remaining path — if this breaks, the feature traps the user);
- toggling in settings is reflected in an already-open chat.

### Phase B7 — connection & security card

**Files:** `features/settings/settings_screen.dart` (mostly),
`data/local/settings_store.dart` (one accessor), `lib/app.dart` (no change —
the pairing route is `'/'`)

Everything this displays is already stored; none of it is shown after pairing,
and `clearFingerprint()` / `clearClientIdentity()` are called from no UI at all
(MADR §6.6).

#### B7.1 Read the state

A single `ConnectionInfo` snapshot assembled in `initState`, so the card does
not fan out awaits across build:

| Row | Source |
|---|---|
| Host | `getHost()` |
| Route | `getRelayUrl()` / `getRelayHostId()` / `getRelayAuthority()` — relay when a url is set, otherwise direct |
| TLS mode + pin | `getPinnedCert(host, deviceId: await getDeviceId())` |
| Client identity | `getClientCertAndKey() != null` → present / absent |

**Pass the real `deviceId` and leave `fallbackToPersistedIdentity` at its
default of false.** The fallback vouches for whichever daemon paired last and
exists for the connect path, which proves its identity by presenting the stored
token; a settings screen has no such proof and would happily display another
daemon's pin as if it were this host's (MADR 0046 H-B). Displaying the wrong
fingerprint in a security card is worse than displaying none.

#### B7.2 Render it

- Fingerprint formatted as the daemon logs it — uppercase hex, colon-separated
  — so it can be compared by eye against `mcremote`'s startup line
  (`cert_fingerprint_sha256=B9:5A:85:…`). Long-press to copy; wrap, never
  ellipsise, or the comparison is impossible.
- TLS mode from the stored `TlsMode` enum (`off`, `selfsigned`, `letsencrypt`,
  `pair_uri.dart:78-81`). `off` should read as a warning, not a neutral value.
- Absent pin renders "not pinned" rather than blank — a blank row reads as a
  loading bug.

#### B7.3 Re-pair this host

A destructive action, scoped:

```dart
await store.clearFingerprint();       // pins + fingerprint + host key
await store.clearClientIdentity();    // client cert + key
// host, device id, token, relay route and all preferences are kept
if (context.mounted) context.go('/'); // ConnectScreen
```

Behind a confirm dialog that states the consequence plainly: the next connection
to this host will trust whatever certificate it presents, so it should only be
used when the host's certificate is known to have changed. That is a real
trust-on-first-use window and the dialog is the only place the user can be told
about it.

Deliberately **not** cleared: the device token. A rotated TLS certificate does
not invalidate the device's pairing, and clearing it would turn a re-pin into a
full re-pair — the exact conflation this row exists to end.

#### B7.4 Tests

`test/connection_card_test.dart` (new):

- pin and TLS mode render, formatted colon-separated uppercase;
- `getPinnedCert` is called **with** the device id and **without** the
  persisted-identity fallback — assert the call arguments; this is the security
  property, not a detail;
- absent pin renders "not pinned"; absent client identity renders "absent";
- relay vs direct route rendering;
- re-pair clears pin + client identity and **keeps** host and token — assert the
  survivors explicitly, mirroring B4's credential-survival test;
- re-pair is not performed when the confirm dialog is cancelled.

---

### Phase C1 — live verification

All four binaries are installed. Repeat the probes that produced the MADR's
numbers and assert against them, then drive the UI paths.

1. **codex** — `model/list` still reports 6 models with 4/5/6-rung ladders;
   `turn/start` with `effort: xhigh` is accepted; a session created with
   "Default thinking level = High" sends `effort: high`, never `ultra`.
   Reproduce with `scratchpad/probe_codex_models.py`.
2. **grok** — a session created with a level spawns
   `grok --reasoning-effort <v> … agent --no-leader stdio` with the flag
   **before** `agent` (MADR 0050 D1); the advertised ladder is
   `low, medium, high` after normalisation; `/thinking` reports the typed
   "new sessions only" error rather than silently succeeding.
3. **opencode / goose** — the negative case. No `thinking_levels` on any model
   option, no `/thinking` in the advertised command list, model picker visually
   unchanged. This is the case a capability bug would break silently.
4. `make preflight`; full `live_grok`, `live_codex`, `live_opencode`,
   `live_goose`; `flutter test`.

**Write the measurements back into the MADR** the way §1 and §2 are written. A
claim about provider behaviour that was not executed does not belong in these
docs.

---

## Risks and sequencing notes

| Risk | Mitigation |
|---|---|
| A1–A3 produce no visible change; easy to mistake for a no-op | Sequence them together and verify via unit tests on captured payloads, not by eye |
| Global default silently escalates cost on a 6-rung model | A5's property test: every resolution is a member of the advertised list, and the intent vocabulary is only low/medium/high (D3) |
| grok argv regression | Extend the existing `live_argv_test.go` pin rather than adding a parallel assertion (MADR 0050 D2) |
| B4's clear wipes credentials | Explicit test that host/token/pins survive; clear is scoped to `tx_cache_v1_*` plus the orphaned `preferred_model_*` |
| Capability gating fails open, showing a thinking chip on goose | C1.3 is the negative test, and `/thinking` is advertised from the interface probe rather than a provider allowlist |
| **B6 changes send behaviour for every existing install** | The stored default is `true` (today's behaviour) and is asserted by name in the tests, not left implicit |
| **B7 displays the wrong host's fingerprint** | `getPinnedCert` is called with the real device id and *without* `fallbackToPersistedIdentity`; the call arguments are asserted (MADR 0046 H-B) |
| **B7's re-pair opens a trust-on-first-use window** | Scoped to pin + client identity, keeps host/token, and the confirm dialog states the consequence rather than just asking "are you sure" |

## Out of scope

- Writing opencode's config file to set `reasoningEffort` (D6).
- Anthropic `thinking.budgetTokens` as a numeric control — a different shape
  from a ladder; revisit only if opencode ever advertises levels.
- opencode **variants** as a level source: user-authored, not advertised, so
  the daemon cannot enumerate them honestly.
- Per-provider default model memory (B1.4) — a follow-up if wanted.
- A reduce-motion setting — investigated and rejected in MADR §6.6's
  runner-up note: the starfield is a static painter and the animated surfaces
  already honour the OS flag.
- "Keep screen awake while a turn runs" — real gap, but notifications already
  answer the same need without holding the screen on.
- Rotating the device token from the connection card: B7 deliberately keeps the
  token, and re-issuing one is a pairing concern, not a settings one.
