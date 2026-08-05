# MADR 0069: macOS permissions — sandbox parity, EPERM honesty, TCC identity

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Proposed — for review, **revision 2 (2026-08-04)**. Revised
  same-day after operator evidence: Q1 confirmed (Linux host has
  `allow_full_access: true`), and the operator observes raw
  **"operation not permitted"** on the phone — not the masked
  "not a directory" the first draft predicted. A live-host audit (daemon
  logs, launchd state, binary signature) plus a propagation trace
  corrected the fact base; F4–F7 and D4/D7 are new or rewritten.
  Not implemented.
- **Date**: 2026-08-04
- **Deciders**: Project Owner
- **Implementation plan**:
  [0069-PLAN-macos-permissions-and-sandbox-parity.md](0069-PLAN-macos-permissions-and-sandbox-parity.md)
  (P0–P6; P0.4's operator half — the live-config line + kickstart — was
  applied 2026-08-04 during review)
- **Scope**: `internal/cli/service/defaults_mcremote.yaml` (setup
  template), `internal/agenterr` + provider error paths (EPERM
  classification), `internal/ws` error logging, `internal/provider/goose`
  (mode flags), `Makefile`/`scripts/install-binary.sh` (opt-in signing),
  docs/runbooks, minor phone error-copy. **No wire-protocol changes**
  (`dangerous` and `errorKind` already exist on the wire). Goose/opencode
  *confinement* (a real sandbox for the HTTP transport) is out of scope —
  named follow-up.
- **Related**:
  [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md)
  (LaunchAgent posture, confirmed correct for TCC; its §TCC note was
  never implemented),
  [0060-MADR-local-unsigned-build-and-install.md](0060-MADR-local-unsigned-build-and-install.md)
  (ad-hoc signing — amended here),
  [0065-MADR-update-automation.md](0065-MADR-update-automation.md)
  (update flow must preserve TCC identity),
  [0044-MADR-auto-approve-modes.md](0044-MADR-auto-approve-modes.md)
  (dangerous-mode semantics; goose deferred item decided here),
  [0048-MADR-codex-sandbox-namespace.md](0048-MADR-codex-sandbox-namespace.md)
  (why Linux enabled `allow_full_access` — the other half of the
  asymmetry).
- **Non-goals**: OS sandboxing for goose/opencode (own MADR); Developer
  ID / notarization / app-bundle distribution; MDM/PPPC (needs enrolled
  MDM); changing the LaunchAgent posture; changing codex's sandbox
  policies themselves.

---

## Problem

The reported symptom: on the macOS host the phone's session mode menu no
longer offers "Full access" (codex), and sessions fail with raw
**"operation not permitted"**; on the Linux host Full access exists and
works; goose on `auto` roams outside the workspace on both.

The investigation resolved this into **three stacked mechanisms** — one
config bug producing the missing menu item, one *by-design* sandbox
producing the visible error, and one latent OS layer (TCC) that produces
the same error string for different reasons and is currently
indistinguishable from it:

1. The mode menu lost "Full access" because of provisioning-template
   drift, not macOS (F1).
2. With Full access unavailable, codex runs its default
   `workspace-write` **Seatbelt** sandbox — and every operation outside
   the workspace correctly fails `EPERM`, surfaced to the phone verbatim
   (F5). On Linux the operator escapes this with Full access
   (`allow_full_access: true`, confirmed); on macOS the escape is
   config-hidden. **The observed error is most plausibly the sandbox
   working as designed with its off-switch missing.**
3. Underneath, TCC produces the *same* `EPERM` string for protected
   paths (`~/Documents`, `~/Desktop`, `~/Downloads`, iCloud) — hitting
   the **daemon's own identity**, because the daemon itself executes
   agent terminal commands and ACP file I/O (F6). No code distinguishes
   the two, nothing logs them, and the daemon's ad-hoc `a.out` identity
   means any Full Disk Access grant dies on the next upgrade (F7).

## Grounding facts

### F1 — The mode menu is config-gated, not platform-gated (confirmed)

The only code that removes a mode named "full access" is codex's gate:
`internal/provider/codex/mode.go:109`
(`!cfg.AllowFullAccess → continue`), config `allow_full_access`
(`internal/config/config.go:579`), default false (`:706-708`). The macOS
host's live config has no such key; the provisioning template
`internal/cli/service/defaults_mcremote.yaml:90-101` omits it while
`configs/config.example.yaml:216` documents it. **Q1 closed by the
operator (2026-08-04): the Linux host's config has
`allow_full_access: true`** — set there because 0048 records that under
AppArmor codex's bwrap sandbox fails and `danger-full-access` was the
only writable mode. No `runtime.GOOS` exists anywhere in the provider or
mode pipeline; the phone renders the daemon's advertisement verbatim
(`apps/mobile/lib/features/chat/chat_screen.dart:2774-2781`).

### F2 — opencode and grok never advertised "Full access" here

opencode's menu is engine-derived plus synthetic `auto`
(`internal/provider/opencode/mode.go:69-82`, `:118-130`); grok is static
`default`/`plan` plus synthetic `auto`
(`internal/provider/acpagent/session.go:1434-1465`). Their bypass mode
is `auto`. Any richer historical menu came from engine versions, not
mcremote.

### F3 — Goose-auto outside the workspace is unimplemented confinement

No confinement exists for the HTTP transport: `FSRoots` is dropped by
design (`internal/daemon/daemon.go:500-502`), the shared goose engine is
spawned with no `cmd.Dir` (`internal/provider/acphttp/provider.go:255`),
the ACP `cwd` is advisory (`internal/provider/acphttp/session.go:174-176`),
and goose's `auto` — the provider default — is not flagged `Dangerous`
(`internal/provider/goose/goose.go:23-28`, `:44`); 0044 deferred exactly
this.

### F4 — The "masked error" from revision 1 is real but *dormant*; the
### validators are stat-only and the errno is destroyed

Both cwd validators stat only the directory node and discard the error:
`internal/provider/acpagent/acpagent.go:482-484`,
`internal/provider/httpagent/session.go:385-387`
(`if st, err := os.Stat(cwd); err != nil || !st.IsDir()` → "not a
directory"). Under TCC, `stat(2)` of a protected directory node
typically **succeeds** (only open/enumeration is denied), so this branch
usually never fires — consistent with the operator seeing raw EPERM and
never "not a directory". When it does fire, the discarded errno makes
correct attribution impossible. codex and acphttp perform **no cwd
validation at all** and fall back to `os.Getwd()`
(`internal/provider/codex/session.go:131-137`,
`internal/provider/acphttp/session.go:112-118`).

### F5 — Live-host evidence: codex runs Seatbelt `workspace-write`; the
### daemon logs contain zero EPERM

`~/Library/Logs/mcremote/mcremote.err.log` (read 2026-08-04): every
codex session logs `codex auto-approve armed … mode=auto
sandbox=workspace-write`; no "operation not permitted" appears anywhere
in daemon logs. mcremote always sends codex a sandbox policy
(`internal/provider/codex/mode.go:51-101` — `workspace-write` default,
`danger-full-access` only via the gated mode; applied
`internal/provider/codex/session.go:247-249`). The codex CLI enforces
`workspace-write` with Apple Seatbelt on macOS: out-of-workspace
operations fail `EPERM` **by design**, surfaced to the phone through the
turn-error path (`internal/provider/codex/session.go:1740-1752`, clipped
to 400 chars) and command cards (`internal/provider/codex/items.go:154-164`).
This is the most plausible source of the observed error in codex
sessions — and its correct remedy is the (currently hidden) Full-access
mode, not an FDA grant.

### F6 — Six verbatim passthrough paths carry raw EPERM to the phone;
### none are operator-visible

Traced end-to-end (2026-08-04); "logged" means at default `info` level:

| # | Path | Where | Logged |
| --- | --- | --- | --- |
| 1 | Daemon-executed terminal output → tool text (grok/ACP): the **daemon process** runs agent shell commands, under the **daemon's TCC profile** | `internal/provider/acpagent/terminal.go:86-88`, `:117-135` → `session.go:1329` | no |
| 2 | ACP fs callbacks: the daemon itself does the agent's reads/writes and returns `*os.PathError` verbatim over JSON-RPC | `internal/provider/acpagent/session.go:1933-1935`, `:1961-1966` (caps advertised `acpagent.go:334`) | no |
| 3 | codex Seatbelt turn errors / command cards (F5) | `codex/session.go:1747`, `items.go:154-164` | no |
| 4 | Spawn failures (`fork/exec …: operation not permitted`) → session-create error → phone | `acpagent/acpagent.go:302`, `httpagent/provider.go:436`, `acphttp/provider.go:269`, `codex/provider.go:308` → `session/manager.go:424` → `ws/server.go:1378-1403`, `:1852-1855` | **no — writeError does not log** |
| 5 | Engine stderr tail spliced into start errors | `httpagent/provider.go:484,516`, `acphttp/provider.go:305,325`, `codex/provider.go:359` | Debug only (`httpagent/provider.go:947-949`; default level `info`, `config.go:627`) |
| 6 | Terminal create failure returned as ACP error | `acpagent/terminal.go:91-93` | no |

The phone renders unclassified error strings raw — it special-cases only
`quota`/`rate_limit` error kinds
(`apps/mobile/lib/data/chat/chat_models.dart:157`,
`apps/mobile/lib/features/chat/chat_bubble.dart:453`). `agenterr.Classify`
is already invoked on four mid-session surfaces
(`acpagent/session.go:369`, `acphttp/session.go:458`,
`opencode/http.go:1353`, `opencode/resync.go:263`) but has no permission
kind. Path 2 is the sharpest TCC exposure: grok file I/O executes under
the daemon's identity even when the agent binary might have its own
grants.

### F7 — Live-host identity: ad-hoc, `Identifier=a.out`; grants cannot
### survive upgrades

`codesign -dv ~/.local/bin/mcremote` (2026-08-04):
`Identifier=a.out`, `Signature=adhoc`, `TeamIdentifier=not set` — the Go
linker default; not even 0060 D3's stable-rebuild story changes the
identity class. TCC keys grants to the code-signing requirement; for
ad-hoc that reduces to the per-build CDHash, so **any real upgrade
revokes an FDA grant** — often without re-prompting (stale rows fail
revalidation). Only a certificate-anchored identity survives rebuilds
(Quinn, <https://developer.apple.com/forums/thread/107546>;
<https://github.com/anthropics/claude-code/issues/55661>). Path and swap
are already stable (`scripts/install-binary.sh:207-224`) — the missing
piece is identity. The daemon runs as a user LaunchAgent
(`launchctl print gui/…`: state = running), which is the correct posture:
children inherit its TCC responsibility
(<https://www.qt.io/blog/the-curious-case-of-the-responsible-process>),
so one grant on `mcremote` covers all spawned agents — and grants given
to Terminal.app cover nothing here.

### F8 — External facts (researched 2026-08-04, unchanged from rev 1)

Headless folder prompts are unreliable — design for silent denial
(Syncthing/Homebrew prior art); there is no API to request FDA and
`tccutil` can only reset; PPPC requires enrolled MDM; the sanctioned
detection is probing a known-protected path for EPERM-despite-exists;
denials log as `TCC deny` in the unified log
(`log stream --predicate 'subsystem == "com.apple.TCC"'`). Claude Code's
versioned-path updater is the canonical cautionary tale
(<https://github.com/anthropics/claude-code/issues/60211>).

## Decision drivers

- One error string, three causes (Seatbelt policy, TCC, ordinary perms):
  the user-facing remedy differs per cause, so classification is the
  core deliverable — 0063's honesty principle at the OS boundary.
- Phone-visible errors must be operator-visible: five of six passthrough
  paths log nothing at default level (F6).
- Same provisioning input ⇒ same mode menu on every OS (F1).
- Grants must survive upgrades before 0065 automates them (F7).
- Self-host constraints: no MDM, free-tier signing at most; 0060's
  zero-dependency build must keep working unsigned.

## Decision outcome

### D1 — Kill the template drift; parity is tested, not assumed

`internal/cli/service/defaults_mcremote.yaml` gains the full commented
codex block including `allow_full_access: false` (explicit, with 0048
context) and grok's `sandbox` key — matching `configs/config.example.yaml`.
A unit test asserts template↔example key parity per provider block so
drift cannot recur. The gate itself stays, default false (0044).

### D2 — Restoring Full access on macOS is a documented operator action

One line on the macOS host: `providers.codex.allow_full_access: true`.
This MADR does not flip it — enabling an approval-bypass mode is an
operator decision. README + runbook (D6) document the key, the
Seatbelt consequence of *not* enabling it (out-of-workspace EPERM in
`auto`/default modes — F5), and the 0049 confirmation flow. For
opencode/grok the record is corrective: their bypass mode is `auto` and
already present (F2).

### D3 — Goose `auto` is flagged dangerous; default becomes `approve`

`internal/provider/goose/goose.go`: `auto` gains `Dangerous: true`;
`DefaultModeID` flips to `approve`. 0044's deferred item, decided —
goose's bypass mode gets the same informed consent as codex's, one tap
per session that wants it. Confinement for the HTTP transport remains a
named follow-up (Non-goals).

### D4 — EPERM becomes classified, attributed, and disambiguated

`internal/agenterr` gains `KindPermission`; classification is inserted
at the F6 seams, cheapest-first:

1. **ACP fs callbacks** (F6 #2): wrap `fs.ErrPermission` before
   returning over JSON-RPC — the daemon's own I/O is where TCC is
   certain and attributable.
2. **Spawn sites** (F6 #4): `%w` the exec error; `writeSessionErr`
   (`internal/ws/server.go:1378-1403`) maps `fs.ErrPermission` →
   stable code `permission_denied`.
3. **Terminal create** (F6 #6) and the **mid-session surfaces** already
   calling `agenterr.Classify` — covered by the new kind for free.
4. **cwd validators** (F4): keep the check, `%w` the error, correct the
   message; codex/acphttp gain the same validation (no more silent
   `os.Getwd()` fallback).
5. **Disambiguation copy**, not guesswork: a `permission_denied` from a
   codex session whose sandbox policy is `workspace-write`/`read-only`
   points at the mode ("this mode sandboxes the agent to the workspace —
   switch modes or enable `allow_full_access`"); the same kind on a
   TCC-protected path under darwin points at Full Disk Access; anything
   else stays a plain permission message. Free-form tool *text*
   (F6 #1, #3 command cards) is left verbatim — scanning agent prose for
   error strings is rejected below.

Phone: `errorKind: permission` renders the actionable copy (same
mechanism as `quota`/`rate_limit`).

### D5 — Startup probe + `mcremote doctor`, instead of a prompt that
### cannot exist

Unchanged from rev 1: stat-probe a known-protected location
(`~/Downloads`) at startup and under `mcremote doctor`; classify
EPERM-despite-exists as "no FDA grant"; one log line + doctor guidance
(grant instructions, `tccutil reset`, unified-log predicate). Never a
hard failure.

### D6 — Identity stability: opt-in real-certificate signing + runbook

Unchanged from rev 1, sharpened by F7: with `MC_CODESIGN_IDENTITY` set
(the Apple Development certificate from the 0067 work qualifies), the
build is signed with a stable identifier (embedded `__info_plist`
bundle id — replacing `a.out`) and an anchor-based designated
requirement, so grants survive upgrades. Unset keeps today's ad-hoc
path byte-for-byte (0060 D1 intact). `docs/ops-macos-tcc.md` (new) is
the runbook; 0065's plan gains the cross-reference (update flow re-signs
with the local identity when configured, or documents the re-grant
cost). Amends 0060 D1 with the middle path it never evaluated: Developer
ID stays rejected; local Apple Development signing is optional.

### D7 — Phone-visible errors become operator-visible (new in rev 2)

`writeSessionErr`/`writeError` log at `info` (code + clipped message +
session/provider), and the engine-stderr splice sites (F6 #5) log the
same tail they hand to the phone at `warn` — closing the "the phone saw
it, the logs did not" class that made this investigation need a
phone screenshot instead of a grep. Bounded: same clipping as the wire
copy; no new levels; no payload logging beyond what already reaches the
phone.

### Rejected

- **Treating the observed EPERM as TCC and leading with FDA guidance.**
  F5 says the dominant cause in codex sessions is Seatbelt
  `workspace-write` — telling users to grant FDA for that would be
  wrong twice (it would not help, and it over-privileges the daemon).
  Classification-with-context (D4.5) instead.
- **Scanning agent tool text for error strings** (F6 #1/#3 prose): the
  daemon does not parse agent prose; only errors the daemon itself
  constructs get classified.
- **Filtering/force-adding modes per platform; flipping
  `allow_full_access` default; LaunchDaemon or app-bundle wrapper;
  programmatic FDA; PPPC-as-requirement; mandatory signing** — as rev 1,
  reasons unchanged.

## Consequences

### Positive

- The same wrong string now yields three different correct remedies —
  mode switch, FDA grant, or ordinary chmod — each stated to the user.
- The menu becomes deterministic per config; the config shows every
  lever; Linux and macOS provisioning converge.
- Grants survive upgrades on signed builds; the trap is documented for
  unsigned ones.
- Operators can grep for what users screenshot.

### Negative / trade-offs

- Goose `auto` costs one confirmation tap per session (deliberate).
- Signing is a manual opt-in prerequisite; unsigned stays default.
- New `info`/`warn` log lines on error paths (bounded, clipped).
- The Seatbelt-vs-TCC disambiguation is heuristic (mode + path
  context); the copy is worded suggestively, never assertively —
  same rule as 0067's local-network guidance.

### Neutral

- No wire changes: `dangerous`, `errorKind`, and error codes are
  existing surfaces; D3/D4 are payload data.
- Linux behaviour: unchanged except the (already-correct) template
  gaining explicit keys.

## Verification

| # | Check | How |
| --- | --- | --- |
| U1 | Template↔example parity test fails on key drift (D1) | go unit |
| U2 | codex mode menu matches config on any GOOS (extend existing) | go unit |
| U3 | goose `auto` dangerous + default `approve`; phone confirmation flow (0049 tests extend) | go unit + dart widget |
| U4 | `KindPermission`: fs-callback, spawn, terminal-create, validator seams each classify an injected `fs.ErrPermission`; `writeSessionErr` maps to `permission_denied` | go unit |
| U5 | Disambiguation: codex+workspace-write context → mode copy; darwin+protected-path context → FDA copy; other → plain | go unit + dart unit |
| U6 | Probe/doctor classify EPERM-despite-exists; absent dir skips | go unit |
| U7 | Error paths log at info/warn with clipped copy (D7) | go unit (log capture) |
| U8 | Signed build: stable identifier + anchor-based DR across two source-changed builds; unsigned path unchanged | live-tagged + manual |
| G1 | macOS host: grant FDA to signed binary, upgrade, session under `~/Documents` works without re-grant; and a codex `auto` session outside the workspace shows the mode-pointing copy, not FDA copy | ops walkthrough |

## Open questions

- ~~Q1: Does the Linux host have `allow_full_access: true`?~~
  **Answered 2026-08-04 by operator: yes.** The asymmetry story is
  closed; engine-version differences for opencode/grok menus remain
  unverified but no longer load-bearing.
- Q2: Does the FDA pane on current macOS accept a bare CLI binary
  (single Dec-2025 refusal report,
  <https://developer.apple.com/forums/thread/118508>)? Check on this
  host during G1.
- Q3: Confirm F5's attribution on this host: one codex `auto` session
  asked to touch a file outside the workspace (expect EPERM), then the
  same op in `full-access` after enabling the gate (expect success,
  no FDA involved) — one session each closes it.
- Q4: Does a TCC-protected workspace (`~/Documents/<proj>`) fail even
  in codex `full-access` (pure TCC shape), and does the daemon-side fs
  callback (grok) EPERM there while a session under `~/gitrepos`
  succeeds? Confirms the D4.5 copy split on hardware.
- Q5: Does goose honour the ACP `cwd` for its shell tools (one `pwd`
  in a goose session)? Feeds the confinement follow-up MADR.
