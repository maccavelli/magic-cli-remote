# MADR 0064: Connect screen — one screen, four steps

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: Proposed — for review, not implemented
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
| 6 | **Host (mcremote) text field** + helper | see D3 |
| 7 | Transport chip or menu | yes |
| 8 | Relay display field | see D3 |
| 9 | **Advanced: long-lived token** expander | see D4 |
| 10 | Test healthz / **Connect** | yes |
| 11 | Status card | yes |

**The Connect button is the tenth of eleven items.** On a phone it sits below
the fold, so the action that completes pairing is the one you have to go
looking for. Sections 3, 6 and 9 are the ones carrying their weight least: 3 is
read once, 6 is almost always filled by the QR, and 9 is a fallback path.

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

### D3 — Remove the Host field from the connect screen

The pairing payload carries the host; the transport control names the endpoint
actually in use. A field that is filled by the QR in every normal flow is
clutter on the screen where clutter costs the most.

**Conditional on D3a** — see Consequences: the transport block must name the
endpoint in *every* state, not only when the menu is showing.

### D4 — Remove "Advanced: long-lived token"; surface it in Settings

A long-lived token (`mcremote pair create`) is an escape hatch, not an
onboarding step. It belongs with the other durable settings, under a
**Long-lived token** entry in Settings, with the same obscured field and
show/hide toggle it has today.

**Conditional on D4a** — see Consequences: Settings must be reachable from the
connect screen.

### D5 — Keep Scan QR, Enter code, Paste, Test healthz, status card

Unchanged. Scan/Enter/Paste are the three ways in and all three are in use;
Test healthz is the diagnostic that answers "is the host even up"; the status
card is where every failure is explained.

### Resulting screen

```text
  logo
  ▸ Connect to your machine — steps        (collapsed)
  [ Scan QR ]  [ Enter code ]
  [ Paste URI / code / token ]
  Transport:  ( Mesh | Relay )  + endpoint
  [ Test healthz ]  [ Connect ]
  status card
```

Eleven sections become seven, and **Connect moves above the fold**.

---

## Consequences, and two things that need deciding

### D3a — Removing the Host field removes a capability, not just clutter

The premise offered for D3 was that Host "is not editable anyway". It **is**
editable today — `connect_screen.dart:1175` is a live `TextField`, and eight
call sites read it (dial target, probe target, relay-authority derivation,
healthz target, the Android cleartext check). Removing it therefore removes
three abilities:

1. **Hand-entering a host.** A LAN or emulator address typed by a developer.
   `_defaultHost()` prefills `127.0.0.1:7531` in debug builds specifically for
   this.
2. **Bare-token pairing.** Pasting `mcr_…` sets only the token. With no host
   field and no stored host there is nothing to dial.
3. **Test healthz before pairing**, which needs a host to probe.

None is fatal — QR and pair-URI flows carry the host, and that is the normal
path — but they should be given up knowingly. **Mitigation:** make Host an
editable field in Settings alongside the long-lived token (D4), so the escape
hatches live together and the onboarding screen stays clean.

**Also required if D3 lands:** the transport block currently names the endpoint
*only* when both transports are available and the menu renders. In the
sole-available state it says "Using Mesh" with no address. With the Host field
gone there would be states where the user cannot see what they are about to
connect to. The chip must carry the endpoint too.

### D4a — Settings is routable pre-pairing, but not reachable

`/settings` is **not** redirected away when unpaired (`app.dart:48-69` bounces
only `/sessions*`), so the route works. But the connect screen's AppBar offers
one action — a popup menu containing "Clear saved credentials" — and no way to
open Settings. Moving the token there without adding an entry point would
strand exactly the users who need it. **Mitigation:** add Settings to the
connect screen's AppBar menu.

### D2a — Dropping "Claim & connect" removes the cue that a code is held

The label was introduced two days ago for a specific reason: with both
transports up, a scanned code is deliberately *held* until a transport is
chosen (0062 D5), and the screen looked like the scan had failed. That is the
exact confusion this project owner reported — *"i mistook the auto-claim not
happening as the QR scan failing"* — and the label was the fix.

Reverting to a single "Connect" reinstates the condition that produced it. The
status line still says "Both transports are up — choose one, then Connect to
claim", so the information is present; it is the *button* that stops carrying
it. Options:

- **(a)** Accept it. The step-list of D1 makes the four-step flow explicit, and
  the status line covers the moment.
- **(b)** Keep "Connect" but show a held code as an explicit step-4 affordance
  (e.g. the status card gaining "Code ready — choose a transport and Connect").
- **(c)** Keep the dynamic label. Rejected by the requirement as stated.

**Recommendation: (b).** It satisfies "one button, always says Connect" while
keeping a visible cue that something is pending.

---

## Alternatives considered

| Option | Why not |
|--------|---------|
| Keep Host but move it into the steps disclosure | Still on the screen, still needs the same "which host?" answer from the transport block — the disclosure just hides it |
| Auto-claim on scan when both transports are up (undo 0062 D5) | Spends a one-shot pair code on a guessed transport; that is what D5 and amendment A1 exist to prevent |
| Move the whole screen to a wizard (one step per page) | Heavier than the problem. Four steps that fit on one screen do not need four screens |

---

## Verification plan

| # | Check | Level |
|---|-------|-------|
| V1 | Connect is visible without scrolling at 640 dp height | widget |
| V2 | Connect claims a held pair code; connects with a token otherwise | widget |
| V3 | Connect is enabled with both transports up and no selection, and dials mesh | widget |
| V4 | The endpoint is visible in every transport state | widget |
| V5 | Settings reachable from the connect screen while unpaired | widget |
| V6 | Long-lived token can be entered in Settings and pairs | hardware |
| V7 | QR → transport → Connect completes pairing end to end | hardware |

---

## Open questions for review

1. **D3a**: accept losing hand-entered Host / bare-token pairing / pre-pairing
   healthz, or reinstate Host as an editable Settings field?
2. **D2a**: which of (a) / (b) for signalling that a code is held?
3. Should **Test healthz** stay on the connect screen once Host is gone, given
   it probes the resolved host rather than a typed one?
4. Should the steps disclosure auto-expand on genuine first run (no stored
   host, no token), and stay collapsed thereafter?
