# MADR 0064: Connect screen — one screen, four steps

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: Proposed — **review round 1 answered 2026-08-02**; D3 withdrawn,
  D4a accepted, D2a superseded by **D6 (Connect mode)**. Not implemented.
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

## Decision outcome (proposed)

### D1 — Instructions collapse into a step-by-step disclosure

Retitle the headline **"Connect to your machine — steps"** and make it an
`ExpansionTile`, collapsed by default, in the same style the Advanced tile uses
today. Inside:

> On the host running mcremote, run:
> `mcremote pair code --name <name of device>`
> to generate the QR and short-term code.

Reclaims two lines of prose from every launch after the first, while keeping
the instructions one tap away for the launch that needs them.

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
Test healthz is the diagnostic that answers "is the host even up"; the status
card is where every failure is explained.

### D6 — **Connect mode**: `Select` (default) or `Auto`

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

Worth fixing regardless of mode: the current deferral triggers on
`bothAvailable` for **any** payload, including a **token** QR. A token is
idempotent — there is nothing to burn and retrying is free — so pausing for a
transport choice buys nothing there. Recommendation: defer only when
`payload.hasCode`, in both modes. That removes a pause from the token flow
without touching the protection that matters.

#### Storage and default

- `SettingsStore` key `connect_mode`, values `select` | `auto`.
- **Default: `select`.** It is today's shipped behaviour, and the safer of the
  two for the credential that can actually be lost. Auto is opt-in for users who
  have decided the extra tap is not worth it.

### Resulting screen

```text
  logo
  ▸ Connect to your machine — steps        (collapsed)
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

- **Auto mode can spend a pair code on a half-up mesh.** Documented in D6; the
  mitigation is that it is opt-in and that Select remains the default. It is not
  eliminated, because Select's own default is Mesh.
- **Two flows to keep working.** Every future change to `_applyPair` now has two
  paths through it. The mitigation is that Auto is defined as *skipping* the
  deferral rather than as a separate code path, so there is one branch, not two
  implementations.
- **Host stays**, so the vertical saving is smaller than the original proposal
  claimed. V1 decides whether that is enough.

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
| V8 | Connect mode persists across restart and defaults to `select` | unit |
| V9 | Settings reachable from the connect screen while unpaired | widget |
| V10 | Long-lived token can be entered in Settings and pairs | hardware |
| V11 | QR → transport → Connect completes pairing end to end, both modes | hardware |

V6 is the one that matters: it is the executable form of "fallback to relay if
mesh offline", and it passes only for the pre-claim failure — which is the
boundary A1 draws.

## Open questions for review

1. **Default mode** — `select` proposed. `auto` is the friendlier first-run
   experience and was the behaviour before 0062; `select` is safer for the one
   credential that can actually be lost. Confirm or flip.
2. **Scope the deferral to codes only** (D6, last subsection)? It removes a
   pointless pause from token QRs and protects nothing less.
3. Should **Test healthz** stay on the connect screen, now that Host stays?
   (It was only in question because Host was leaving.)
4. Should the steps disclosure auto-expand on genuine first run — no stored
   host, no token — and stay collapsed thereafter?
