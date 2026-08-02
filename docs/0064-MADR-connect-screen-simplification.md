# MADR 0064: Connect screen — one screen, four steps

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: Proposed — **review rounds 1–2 answered 2026-08-02**; all open
  questions closed. D3 withdrawn, D4a accepted, D2a superseded by **D6 (Connect
  mode, default `auto`)**, and **D7 (burnt-code recovery)** added as D6's paired
  mitigation. Not implemented.
- **Date**: 2026-08-02
- **Deciders**: Project Owner
- **Scope**: The pre-pairing connect screen (`apps/mobile/lib/features/connect/`)
  and whatever moves to Settings as a result. No client, protocol, daemon or
  transport-policy changes.
- **Related**:
  [0062-MADR-phone-transport-selection.md](0062-MADR-phone-transport-selection.md)
  (transport menu, QR deferral D5),
  [0046-MADR](0046-MADR-reconnect-and-pairing-hardening.md) (per-daemon pair
  hints, M-2).

---

## Problem

The connect screen is the first thing a new user sees and it has accumulated
eleven vertical sections:

| # | Section | Still needed? |
|---|---------|---------------|
| 1 | Logo (96 px) | yes |
| 2 | "Connect to your machine" headline | yes |
| 3 | Two-line host-command instructions | only on first run |
| 4 | Scan QR / Enter code | yes — the primary actions |
| 5 | Paste URI / code / token | yes |
| 6 | Host (mcremote) text field + helper | **kept** — D3 withdrawn |
| 7 | Transport chip or menu | yes |
| 8 | Relay display field | kept |
| 9 | **Advanced: long-lived token** expander | see D4 |
| 10 | Test healthz / **Connect** | yes |
| 11 | Status card | yes |

**The Connect button is the tenth of eleven items.** On a phone it sits below
the fold, so the action that completes pairing is the one you have to go
looking for. Sections **3** and **9** are the ones carrying their weight least:
3 is read once and 9 is a fallback path. (6 was also proposed for removal; see
D3 — withdrawn on review.)

The flow the screen should express is four steps:

1. Generate a code/QR (or token) on the host
2. Scan / enter / paste it on the phone
3. Choose a transport — or accept the mesh default
4. Connect

---

## Decision outcome

### D1 — Instructions collapse into a step-by-step disclosure

Retitle the headline **"Connect to your machine - Steps"** and make it an
`ExpansionTile`, collapsed by default, in the same style the Advanced tile uses
today. Inside:

> On the host running mcremote, run:
> `mcremote pair code --name <name of device>`
> to generate the QR and short-term code.

Reclaims two lines of prose from every launch after the first, while keeping
the instructions one tap away for the launch that needs them.

**Collapsed always** (decided on review): no auto-expand on first run. A
disclosure that sometimes opens itself is a disclosure the user stops trusting
to stay shut, and the four-step flow is legible from the buttons themselves.

### D2 — Connect performs the claim, and is never gated on transport

- The button reads **"Connect"** in all states.
- If a scanned/pasted pair code is held, Connect **claims** it; otherwise it
  connects with the token.
- Connect is enabled regardless of whether a transport has been chosen. With
  both transports available and no explicit pick, the dial uses **mesh**.

**Already true, and stays true:** `_effectiveTransport` resolves to
`_selection ?? TransportMode.mesh` when both are available, so no gate exists
today. This decision records it so it is not reintroduced.

### D3 — ~~Remove the Host field~~ **WITHDRAWN**

Proposed, reviewed, **not taken**. The field stays exactly as it is.

The premise was that Host is not editable; it is (`connect_screen.dart:1175`),
and removing it would have cost hand-entered hosts, bare-token pairing and
pre-pairing healthz for a few reclaimed pixels. Keeping it also retires the
follow-on requirement that the transport chip name the endpoint in every state
— with Host on screen, the endpoint is always visible.

Revisit only if the screen is still too long after D1, D4 and D6 land.

### D4 — Remove "Advanced: long-lived token"; surface it in Settings

A long-lived token (`mcremote pair create`) is an escape hatch, not an
onboarding step. It belongs with the other durable settings, under a
**Long-lived token** entry in Settings, with the same obscured field and
show/hide toggle it has today.

**Accepted with D4a**: a **Settings** action is added to the connect screen's
AppBar menu, next to "Clear saved credentials". `/settings` is already routable
while unpaired (`app.dart:48-69` bounces only `/sessions*`); it simply had no
door. Without that door, moving the token to Settings would strand exactly the
users who need it.

### D5 — Keep Scan QR, Enter code, Paste, Test healthz, status card

Unchanged. Scan/Enter/Paste are the three ways in and all three are in use;
**Test healthz stays** (confirmed on review) — it is the diagnostic that
answers "is the host even up", and with Host kept (D3 withdrawn) it still has a
typed target to probe. The status card is where every failure is explained.

### D6 — **Connect mode**: `Auto` (default) or `Select`

A Settings option, **Connect mode**, with two values that decide what happens
the moment a QR is scanned or a code entered:

| Mode | Behaviour when **both** transports are available |
|------|--------------------------------------------------|
| **Select** | Populate, run the probes, then **stop**. User picks a transport, then Connect claims. Today's behaviour (0062 D5). |
| **Auto** | Populate, run the probes, then **claim immediately** over **mesh**, falling back to relay if mesh is not usable. Pre-0062 behaviour, restored deliberately. |

Neither mode changes anything when only one transport is available — that case
already auto-starts on the sole available path, in both modes.

This supersedes D2a. Instead of guessing whether the held-code cue is worth its
friction, the user decides which trade they want.

#### What it costs to build: one branch

The deferral is a single early return in `_applyPair`
(`connect_screen.dart`, the `if (_availability.bothAvailable)` block). Auto mode
skips it. Everything downstream already exists and is unchanged:

- probes still run (0062 D2), so "mesh offline" is already known;
- `resolveInteractive` already defaults to **mesh** when both are available;
- the DialEpisode already falls back mesh → relay on a retryable failure
  (0062 D4), which is exactly "fallback to relay if mesh offline".

So Auto is not new machinery — it is *declining to pause*. That is why it is
cheap, and also why it is safe to offer: the fallback the requirement asks for
is the fallback that already ships.

#### The one real difference, stated honestly

Amendment A1 (0062) forbids hopping transports once a pair code is **on the
wire**. So in Auto mode:

- mesh **refuses the connection** (socket never opens) → fallback to relay
  works, code intact;
- mesh **accepts, then dies after the claim is sent** (the half-up tailnet case
  0062 §0.3 calls out) → **no fallback, and the code is spent**.

Select mode does **not** eliminate that risk — its default highlight is Mesh,
so a user who taps Connect without changing it burns the code identically. What
Select buys is *the opportunity to choose relay* when you already know the mesh
is flaky. That is a smaller difference than it first appears, and it is the
honest reason to make this a preference rather than a policy.

#### Deferral is only load-bearing for one-shot codes

**Accepted on review.** The current deferral triggers on `bothAvailable` for
**any** payload, including a **token** QR. A token is idempotent — there is
nothing to burn and retrying is free — so pausing for a transport choice buys
nothing there. Select mode will defer only when `payload.hasCode`; a token QR
proceeds straight to Connect in both modes.

#### Storage and default

- `SettingsStore` key `connect_mode`, values `select` | `auto`.
- **Default: `auto`** (decided on review; `select` was proposed).

Auto is the smoother first-run flow and was the behaviour before 0062: scan,
and you are in. The extra tap only earns its keep for a user who already knows
their mesh is unreliable, and that user can switch to Select.

**This makes the burn case the default path**, which is why D7 is not polish
but the other half of this decision. The gap is narrower than it looks — Select
highlights Mesh by default too, so a user who taps Connect without changing it
burns the code identically — but "narrower" is not "absent", and defaulting to
Auto means the recovery experience has to be genuinely good.

### D7 — Burnt-code recovery: say it loudly, say what to do

A pair code is one-shot. If connectivity dies in the window between the claim
leaving the phone and `pair_ok` arriving, the host has consumed the code and
minted a token the phone never received. **No client change can prevent this** —
it is a property of a one-shot credential over an unreliable link — so the only
thing left is to make the failure legible and the recovery immediate.

Today this is handled correctly but *quietly*: `_handleConnectFailure` appends
an explanation to the status **card**, which sits at the bottom of a screen the
user already has to scroll. A failure the user must act on is the last thing
that should be below the fold.

#### The notification

Surface it through the existing `showTopNotification`
(`theme/top_notification.dart:80`) — it already renders at the top over the root
overlay, queues rather than replaces, carries a severity, and supports an
action:

```dart
showTopNotification(
  context,
  'That pair code has been used. Get a new one and try again.',
  severity: NoticeSeverity.error,
  actionLabel: 'Enter code',
  onAction: _enterCode,
);
```

**Copy rules.** Plain, specific, and it ends with the next action:

| Bad | Why |
|-----|-----|
| "pair_failed: claim aborted" | Machine text; nothing to do with it |
| "Something went wrong" | True and useless |
| "The pair code may have been used…" | Hedged. The user cannot resolve "may"; treat it as used |

**Chosen copy:** *"That pair code has been used. Get a new one and try again."*
Definite rather than hedged — a code that might be spent is worthless either
way, and telling someone to retry a code that may not work wastes the one
recovery attempt they have.

#### One tap back to recovery

The notification's action opens the **Enter code** dialog directly. The failure
and the fix are then one tap apart, which is the whole point of surfacing it at
the top.

The status card keeps the longer explanation (including the exact host command),
so the detail is available without the notification having to carry it.

#### Observability

Since we cannot prevent it, we should at least be able to count it.

- **Client**: a single stable log line on the spent-credential path —
  `mcremote: pair code spent without token (transport=…, code=…)` — so a bug
  report contains the fact rather than a description of it.
- **Host**: a burn leaves a *real device* registered that no phone holds a token
  for. `mcremote pair list` shows it; `mcremote pair revoke <id>` or
  `mcremote pair prune` clears it. This is the operator-visible fingerprint of
  the failure and should be documented next to the message, so the orphan is
  recognised for what it is rather than treated as a mystery device.

#### Not offered: "Try Mesh / Try Relay"

Already withheld once a code is spent (0062 A1). Retrying a burnt code on
another transport can only earn a permanent `invalid_code`, so the button is
absent by design and the notification does not reintroduce it.

### Resulting screen

```text
  logo
  ▸ Connect to your machine - Steps        (collapsed)
  [ Scan QR ]  [ Enter code ]
  [ Paste URI / code / token ]
  Host (mcremote)                          (kept — D3 withdrawn)
  Transport:  ( Mesh | Relay )
  [ Test healthz ]  [ Connect ]
  status card
```

Eleven sections become **eight**: the instructions fold into D1's disclosure and
the Advanced token tile leaves for Settings. Host stays, so the saving is
smaller than first proposed — roughly three lines of prose plus a collapsed
expander. Whether that is enough to lift Connect above the fold on a 640 dp
screen is **V1**, and if it is not, D3 comes back on the table.

---

## Consequences

**Good**

- Connect moves up the screen; the four-step flow is stated once, in D1's
  disclosure, instead of implied by layout.
- The held-code question stops being a design guess and becomes a user
  preference (D6).
- Settings gains a door from the landing screen (D4a), which also makes the
  long-lived token and any future pre-pairing setting reachable at the moment
  they are needed.
- Token QRs stop pausing for a choice that protects nothing (D6, deferral scoped
  to codes).

**Costs and risks**

- **Auto is the default, so the burn case is the default path.** Not opt-in any
  more, which raises the stakes on D7. It is not eliminated by choosing Select
  either — Select's own default is Mesh — but Auto removes the moment at which a
  user who *knows* their mesh is flaky would have picked Relay. Accepted
  deliberately: the flow is smoother for everyone, and the recovery is one tap.
- **Two flows to keep working.** Every future change to `_applyPair` now has two
  paths through it. The mitigation is that Auto is defined as *skipping* the
  deferral rather than as a separate code path, so there is one branch, not two
  implementations.
- **Host stays**, so the vertical saving is smaller than the original proposal
  claimed. V1 decides whether that is enough.
- **A failure we cannot fix now has a UI surface to maintain.** D7 adds copy,
  an action and a log line that must stay correct as the claim path changes.
  That is the price of making an unpreventable failure legible.

## Alternatives considered

| Option | Why not |
|--------|---------|
| Keep Host but move it into the steps disclosure | Still on the screen, still needs the same "which host?" answer from the transport block — the disclosure just hides it |
| Auto-claim on scan, unconditionally (undo 0062 D5 for everyone) | Spends a one-shot pair code on a guessed transport with no way to opt out. **D6 offers it as a choice instead**, defaulted off |
| Move the whole screen to a wizard (one step per page) | Heavier than the problem. Four steps that fit on one screen do not need four screens |

---

## Verification plan

| # | Check | Level |
|---|-------|-------|
| V1 | Connect is visible without scrolling at 640 dp height | widget |
| V2 | Connect claims a held pair code; connects with a token otherwise | widget |
| V3 | Connect is enabled with both transports up and no selection, and dials mesh | widget |
| V4 | **Select** mode: a dual-available code QR does **not** claim until Connect | widget |
| V5 | **Auto** mode: a dual-available code QR claims immediately, over mesh | widget |
| V6 | **Auto** mode with mesh refusing the socket: falls back to relay, code intact | fake relay |
| V7 | A **token** QR never pauses for a transport choice, in either mode | widget |
| V8 | Connect mode persists across restart and defaults to `auto` | unit |
| V9 | Settings reachable from the connect screen while unpaired | widget |
| V10 | Long-lived token can be entered in Settings and pairs | hardware |
| V11 | QR → transport → Connect completes pairing end to end, both modes | hardware |
| V12 | A spent-code failure raises the **top notification**, with the Enter-code action | widget |
| V13 | That notification does **not** offer Try Mesh / Try Relay (0062 A1) | widget |
| V14 | The spent-code log line is emitted exactly once per burn | unit |
| V15 | An orphaned device appears in `mcremote pair list` after a burn and `pair prune` clears it | hardware |

V6 is the one that matters: it is the executable form of "fallback to relay if
mesh offline", and it passes only for the pre-claim failure — which is the
boundary A1 draws.

## Review decisions (closed 2026-08-02)

| # | Question | Decision |
|---|----------|----------|
| 1 | Remove the Host field? | **No** — D3 withdrawn; it is editable and load-bearing |
| 2 | Settings reachable from the landing screen? | **Yes** — AppBar action (D4a) |
| 3 | How to signal a held code? | **Neither (a) nor (b)** — replaced by D6, a user-chosen Connect mode |
| 4 | Default Connect mode | **`auto`** — smoother first run; D7 is the paired mitigation |
| 5 | Scope the deferral to codes only? | **Yes** — token QRs never pause (D6) |
| 6 | Keep Test healthz? | **Yes** (D5) |
| 7 | Auto-expand the steps disclosure on first run? | **No** — always collapsed (D1) |
| 8 | Burnt-code handling | **D7** — top notification, definite copy, one-tap recovery, client log + host-side orphan trail |

Nothing outstanding. Ready to implement.
