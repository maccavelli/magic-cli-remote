# MADR 0069: macOS permissions — TCC handling and provider mode parity

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Proposed — for review. Not implemented. Triggered by a live
  symptom (2026-08-04): the phone's session mode menu no longer offers
  "Full access" when the daemon runs on macOS (codex/opencode/grok), while
  the Linux host shows it; goose on `auto` meanwhile operates outside the
  workspace. Investigation resolved the symptom to config drift — but the
  audit it forced surfaced the real macOS permissions debt (TCC), which
  this MADR decides alongside the fix.
- **Date**: 2026-08-04
- **Deciders**: Project Owner
- **Scope**: `internal/cli/service/defaults_mcremote.yaml` (setup
  template), `internal/provider/*` (error classification, mode flags),
  `Makefile`/`scripts/install-binary.sh` (signing), docs/runbooks. **No
  wire-protocol changes** (the `dangerous` flag already exists on the mode
  wire type). Goose *confinement* (a real sandbox) is out of scope — named
  as follow-up.
- **Related**:
  [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md)
  (the LaunchAgent posture this builds on; its §TCC note was never
  implemented),
  [0060-MADR-local-unsigned-build-and-install.md](0060-MADR-local-unsigned-build-and-install.md)
  (ad-hoc signing decision — amended here for TCC),
  [0065-MADR-update-automation.md](0065-MADR-update-automation.md)
  (self-update must preserve TCC identity),
  [0044-MADR-auto-approve-modes.md](0044-MADR-auto-approve-modes.md)
  (dangerous-mode semantics; goose left-untouched item picked up here),
  [0048-MADR-codex-sandbox-namespace.md](0048-MADR-codex-sandbox-namespace.md)
  (why the Linux host enabled `allow_full_access`).
- **Non-goals**: building an OS sandbox for goose/opencode (own MADR);
  Developer ID / notarization / App Store distribution of the daemon;
  MDM/PPPC deployment (requires user-approved MDM enrollment — not a
  self-host path); changing 0058's LaunchAgent-not-LaunchDaemon posture
  (TCC confirms it: a root daemon can never be prompted).

---

## Problem

Two distinct problems wear the same "permissions on macOS" clothing:

1. **Mode-menu asymmetry (the reported symptom).** The macOS daemon's
   codex sessions no longer offer "Full access". Suspicion naturally falls
   on macOS. It is not macOS: it is the provisioning template silently
   diverging from the example config, interacting with a config gate that
   defaults closed.
2. **TCC (the real macOS permissions debt).** The daemon runs headless
   under launchd and spawns agents into any directory the phone picks.
   When that directory is TCC-protected (`~/Documents`, `~/Desktop`,
   `~/Downloads`, iCloud Drive), macOS denies access with `EPERM` —
   silently, since a headless LaunchAgent cannot reliably prompt — and the
   daemon currently converts that denial into the factually wrong error
   "cwd is not a directory". Worse, any grant the operator does make is
   keyed to the binary's code identity, which the Go linker regenerates on
   every build: **every genuine upgrade silently revokes Full Disk
   Access.** None of this has ever fired visibly — the operator's projects
   simply haven't lived under protected paths yet — which is exactly why
   it must be decided before it does.

## Grounding facts (verified against the tree and live config, 2026-08-04)

### F1 — The mode menu is config-gated, not platform-gated

The only code in the repository that removes a mode named "full access"
is codex's config filter:
`internal/provider/codex/mode.go:109` (`if m.mode.ID == modeFullAccess &&
!cfg.AllowFullAccess { continue }`), fed from `allow_full_access`
(`internal/config/config.go:579`), default **false** (`:706-708`). The
macOS host's live `~/.config/mcremote/config.yaml` codex block has no
`allow_full_access` key. The `setup-service` provisioning template
`internal/cli/service/defaults_mcremote.yaml:90-101` **omits the key**,
while `configs/config.example.yaml:216` documents it — template drift.
The Linux host shows the mode because 0048 documents that under AppArmor
its bwrap sandbox fails and `danger-full-access` was the only writable
codex mode — that operator was pushed into `allow_full_access: true`.
There is **zero** platform-conditional code in the mode pipeline: no
`runtime.GOOS` in any provider, no `*_darwin.go` under
`internal/provider/`, and the phone renders whatever the daemon
advertises verbatim (`apps/mobile/lib/features/chat/chat_screen.dart:2774-2781`).

### F2 — opencode and grok never had a "Full access" mode here

opencode's menu is engine-derived (`GET /agent` →
`internal/provider/opencode/mode.go:69-82`) plus a synthetic `auto`
(`:118-130`); grok is static `default`/`plan` plus synthetic `auto`
(`internal/provider/grok/grok.go:44-47`,
`internal/provider/acpagent/session.go:1434-1465`). `git log -S"full
access"` touches only codex/protocol commits. Their unsandboxed mode is
named `auto`. Any richer list previously seen came from the engines'
own catalogs (engine versions differ per host), not from mcremote.

### F3 — Goose-auto outside the workspace is unimplemented confinement,
### not a regression

No confinement exists for goose structurally: the HTTP transport drops
`FSRoots` by design (`internal/daemon/daemon.go:500-502`), `goose.Config`
has no roots field, the shared engine is spawned with **no `cmd.Dir`**
(`internal/provider/acphttp/provider.go:255` — it inherits the daemon's
`$HOME`), and the per-session ACP `cwd` is advisory
(`internal/provider/acphttp/session.go:174-176`). The only path check in
the tree (`pathWithinRoots`, `internal/provider/acpagent/session.go:1970-2007`)
is grok-only, gated on `FSRoots`, and self-described as "a policy gate
and audit surface, not a sandbox". Meanwhile goose's `auto` is the
provider **default** and is not flagged `Dangerous`
(`internal/provider/goose/goose.go:23-28`, `:44`) — 0044 recorded this
("every goose session on the phone starts with no human in the loop") as
an open item and deferred it.

### F4 — TCC denials are currently masked by a wrong error message

There is no TCC-aware code in the repository: no `os.IsPermission` /
`fs.ErrPermission` / `EPERM` classification anywhere in `internal/` or
`cmd/`, no FDA documentation, no probe. The phone-chosen cwd flows
unvalidated into the agent process (`internal/ws/server.go:1146` length
check only → `internal/provider/acpagent/acpagent.go:289` `cmd.Dir`).
Both cwd validators discard the stat error:
`internal/provider/acpagent/acpagent.go:482-484` and
`internal/provider/httpagent/session.go:385-387` report
`"cwd %q is not a directory"` for **any** stat failure — including the
`EPERM` a TCC denial produces. 0058's sole TCC paragraph
(`0058-MADR:213`) prescribed documentation "only if operators hit
'Operation not permitted'" — a reactive trigger nothing can detect,
which therefore never fired. The daemon's own state paths
(`~/.config`, `~/.local/*`, `~/Library/Logs`) are TCC-clean; only
agent project I/O is exposed.

### F5 — Code identity churn silently revokes grants across upgrades

The binary is ad-hoc signed by the Go linker (mandatory on darwin/arm64;
fresh CDHash every build — <https://go.dev/doc/go1.16>). TCC keys grants
to a compiled code-signing requirement; for ad-hoc binaries that reduces
to the per-build CDHash, so **a grant made today dies on the next real
upgrade** — observed in the wild as stale allow-rows that fail
revalidation without re-prompting. 0060 D3 stabilized the CDHash across
*no-change rebuilds* (`Makefile:13-18`) and documented the mechanism —
but framed it as an Application Firewall annoyance and left the TCC half
dormant (`0060-MADR:118-128`). The install path is already stable with
atomic swap (`scripts/install-binary.sh:207-224`) — good; the 0065
update flow (unimplemented) would inherit the identity problem
unmitigated. A stable ad-hoc *identifier* does not help; only a real
certificate identity yields an anchor+identifier designated requirement
that survives rebuilds (Quinn,
<https://developer.apple.com/forums/thread/107546>;
<https://github.com/anthropics/claude-code/issues/55661>).

### F6 — External facts the design leans on (researched 2026-08-04)

- **Responsibility inheritance**: processes launched by launchd are
  their own responsible process; children via fork/spawn inherit it —
  one grant on `mcremote` covers every spawned agent CLI
  (<https://www.qt.io/blog/the-curious-case-of-the-responsible-process>).
  Grants given to Terminal.app do nothing for the LaunchAgent path.
- **Design for silent denial**: whether a headless LaunchAgent ever gets
  a folder prompt is unreliable in practice (Syncthing/Homebrew prior
  art: hard `EPERM`, no prompt —
  <https://forum.syncthing.net/t/operation-not-permitted-after-removing-the-manual-install-and-installing-with-brew/20270>).
- **No request API exists**: FDA can only be granted manually in System
  Settings → Privacy & Security; `tccutil` can only *reset*; PPPC
  profiles require user-approved MDM enrollment
  (<https://developer.apple.com/forums/thread/107546>,
  <https://ss64.com/mac/tccutil.html>).
- **Sanctioned detection**: probe a known-protected path and classify
  `EPERM`-despite-existence as "no grant" (Quinn's pattern, thread
  107546). Denials also log as `TCC deny` in the unified log
  (`log stream --predicate 'subsystem == "com.apple.TCC"'`).
- **Prior art in this exact shape**: Claude Code's versioned-path
  auto-update broke grants daily; its fix list — stable path, atomic
  replace, stable signing identity — is the corroborated playbook
  (<https://github.com/anthropics/claude-code/issues/60211>).

## Decision drivers

- Honest errors over silent failure (0063's principle applied to the OS
  boundary): an `EPERM` must never present as "not a directory".
- The mode menu must reflect *decided* policy, not provisioning
  accidents — and the same provisioning input must yield the same menu
  on both platforms.
- Grants the operator makes must survive upgrades, or 0065's update
  automation will train users that macOS "randomly breaks".
- Self-host constraints: no MDM, no Apple review, free-tier signing at
  most (an Apple Development certificate exists on the build Mac from
  the 0067 iOS work).
- 0044's honesty rule for dangerous modes applies to goose's `auto`
  exactly as it applied to codex.

## Decision outcome

### D1 — Kill the template drift; parity is tested, not assumed

`internal/cli/service/defaults_mcremote.yaml` gains the full commented
codex block including `allow_full_access: false` (explicit, with the
0048 context in the comment) and grok's `sandbox` key — matching
`configs/config.example.yaml`. A unit test asserts the template's keyset
per provider block is a subset-with-same-defaults of the example config,
so the two can never drift silently again. The `allow_full_access`
gate itself is **kept, default false** — it is working as designed
(0044); the defect was that the template hid its existence.

### D2 — The macOS symptom is an operator action, documented

Restoring "Full access" for codex on the macOS host is one explicit
line: `providers.codex.allow_full_access: true`. This MADR does not
flip it — enabling a dangerous mode is an operator decision. The README
codex section and the ops runbook (D6) document the key, its default,
and the phone-side behaviour (mode appears, flagged dangerous, 0049
confirmation applies). For opencode/grok the record is corrective, not
remedial: their full-bypass mode is `auto` and already present; no
"Full access" entry ever existed to restore (F2).

### D3 — Goose `auto` is flagged dangerous (0044's deferred item, decided)

`internal/provider/goose/goose.go`: `auto` gains `Dangerous: true`, and
the provider default flips to `approve` (a session then *opts into*
auto via the existing dangerous-mode confirmation flow, 0049). This is
the honesty half of F3 — the phone currently starts goose sessions
auto-approving, unconfined, with no dialog, which contradicts how every
other provider's bypass mode is presented. Actual *confinement* for the
HTTP transport (workspace roots for goose/opencode) stays a named
follow-up (Non-goals) — flagging is shippable now; sandboxing is a
design of its own.

### D4 — `EPERM` becomes a first-class, correctly-attributed error

Both cwd validators distinguish the stat error: `fs.ErrPermission` (and
`EPERM` from deeper I/O where cheap to classify) maps to a typed
`cwd_permission_denied` error carrying macOS-specific guidance; other
stat failures keep (a corrected form of) the existing message. The
phone's error mapping renders it actionably: on a macOS host, "macOS is
blocking access to this folder. Grant Full Disk Access to `mcremote`
in System Settings → Privacy & Security, or choose a path outside
Documents/Desktop/Downloads." `internal/agenterr` is the natural home.

### D5 — Startup probe + `doctor` surface, instead of a prompt that
### cannot exist

The daemon cannot request access (F6). It can *know*: at startup (and
under a new `mcremote doctor` diagnostic), stat one existing
known-protected location (`~/Downloads` — universally present,
TCC-protected) and classify `EPERM` as "no FDA grant". The result is a
single startup log line and a `doctor` section with the grant
instructions and `tccutil reset` recovery — never a hard failure, since
sessions outside protected paths are unaffected. `/v1/hello` gains no
new field (operator-facing, not phone-facing, until real demand).

### D6 — Identity stability: optional real-certificate signing + runbook

The Makefile gains an opt-in signing step: when `MC_CODESIGN_IDENTITY`
is set (the free Apple Development certificate from the 0067 iOS work
qualifies), the built binary is signed with it plus an embedded
`__info_plist` bundle identifier — producing an anchor-based designated
requirement so **TCC grants survive rebuilds and updates**. Unset, the
build stays exactly today's ad-hoc path (0060 D1 intact for
contributors without certificates). `docs/ops-macos-tcc.md` (new)
becomes the runbook: how grants are keyed, the upgrade-revocation trap
when unsigned, granting FDA to a bare binary, `tccutil reset` after
identity changes, and the unified-log predicate for confirming a TCC
denial. 0065's plan gains a cross-reference: the update flow must
`codesign` the downloaded binary with the local identity when
configured, or document the re-grant cost. This amends 0060 D1's
"reject Developer ID" with a middle path 0060 never evaluated:
Developer ID remains rejected; *Apple Development* signing is optional
and local-only.

### Rejected

- **Filtering or force-adding modes per platform in the daemon.** The
  pipeline's platform-neutrality is a feature (F1); policy belongs in
  config, identically on every OS.
- **Flipping `allow_full_access` default to true.** 0044/0048 chose
  closed-by-default for approval-bypass modes; a provisioning-template
  bug is no reason to weaken it.
- **A LaunchDaemon or app-bundle wrapper for prompt delivery.**
  Contradicts 0058; a root daemon can never be prompted at all, and a
  wrapper app is distribution machinery this project rejected (0060).
- **Auto-granting / programmatic FDA.** Does not exist; anything
  claiming otherwise (e.g. a `com.apple.security.files.all`
  entitlement) is uncorroborated by Apple documentation.
- **PPPC/MDM profiles.** Rejected as a requirement — needs enrolled
  MDM; noted in the runbook for users who happen to have it.
- **Mandatory signing.** Breaks 0060's zero-dependency local build for
  anyone without an Apple ID; opt-in keeps both worlds.

## Consequences

### Positive

- The mode menu becomes deterministic per config, and the config file
  finally shows every lever with its default.
- A TCC denial produces a correct, actionable message at the exact
  moment it happens, plus a proactive startup signal — instead of a
  misleading "not a directory" hunt.
- With one env var set at build time, FDA grants become durable across
  upgrades — removing the trap before 0065 automates upgrades.
- Goose sessions get the same informed-consent treatment as every other
  provider's bypass mode.

### Negative / trade-offs

- Goose users get one extra tap per session that wants `auto` (D3
  default flip) — deliberate friction, same as codex/opencode.
- Signing adds a manual prerequisite (certificate in the login
  keychain) for operators who opt in; unsigned operators keep the
  re-grant-after-upgrade cost, now documented instead of silent.
- The probe (D5) touches `~/Downloads` at startup — benign, but it
  will *itself* appear as a TCC denial event in the unified log on
  ungranted systems; the runbook says so.

### Neutral

- No wire changes: `dangerous` already exists on the mode type
  (`internal/event/event.go:333-350`), so D3 is payload data, not
  protocol.
- Linux behaviour is untouched by every decision here.

## Verification

| # | Check | How |
| --- | --- | --- |
| U1 | Template↔example parity test fails on key drift (D1) | go unit |
| U2 | codex modes with/without `allow_full_access` — menu content matches config on any GOOS | go unit (exists partially; extend) |
| U3 | goose `auto` advertised `dangerous:true`, default `approve`; phone confirmation flow triggers (0049 tests extend) | go unit + dart widget |
| U4 | cwd stat `EPERM` → `cwd_permission_denied` with macOS guidance; `ENOENT`/`ENOTDIR` keep corrected plain message | go unit (injected statFn) |
| U5 | Probe classifies EPERM-despite-exists as no-grant; absent dir skips cleanly; `doctor` renders guidance | go unit |
| U6 | Signed build (identity set): designated requirement is anchor-based and stable across two builds with source changes; unsigned build byte-path unchanged | go unit (live-tagged, needs cert) + manual |
| G1 | On the macOS host: grant FDA, upgrade the binary (signed path), session in `~/Documents` works without re-grant; unsigned path re-asks as documented | hardware/ops (runbook walkthrough) |

## Open questions

- Q1: Does the Linux host's config confirm `allow_full_access: true`
  (closing the asymmetry story), and do its engine versions explain the
  richer opencode/grok menus? (`mcremote version`, `opencode --version`
  on both hosts.)
- Q2: On current macOS (Tahoe), does the FDA pane still accept a bare
  CLI binary via drag/"+"? (One Dec-2025 report of refusal —
  <https://developer.apple.com/forums/thread/118508>; needs a check on
  this host.)
- Q3: Does a headless LaunchAgent session-create into `~/Documents`
  *ever* produce a user-visible prompt on this host, or is it always
  silent? (Determines how strongly the runbook words the "grant it
  pre-emptively" advice.)
- Q4: Does goose honour the ACP `cwd` for its shell tools, or execute
  relative to the engine's inherited `$HOME`? (One `pwd` in a goose
  session answers it; feeds the confinement follow-up MADR.)
