# macOS privacy (TCC) runbook for mcremote

<!-- markdownlint-disable MD013 -->

The macOS sibling of the platform ops docs; decision record:
[MADR 0069](0069-MADR-macos-permissions-and-sandbox-parity.md) (D5/D6).
Read this when an agent session fails with **"operation not permitted"** —
and note that string has **three distinct causes** with three different
remedies:

| Cause | Where it appears | Remedy |
|---|---|---|
| Codex sandbox (`workspace-write`/`read-only`) | Codex sessions touching paths outside the workspace, any host OS | Switch session modes; offer "Full access" via `providers.codex.allow_full_access: true` |
| macOS privacy protection (TCC) — this document | Any provider, paths under `~/Documents`, `~/Desktop`, `~/Downloads`, iCloud Drive | Grant the daemon Full Disk Access (below) |
| Ordinary file permissions | Anywhere | `chmod`/`chown` |

The daemon classifies these where it can (`permission_denied` +
context-specific guidance, 0069 D4) — but tool *output* passes through
verbatim, so raw strings still appear inside agent transcripts.

## How TCC decides, in three facts

1. **Identity, not path.** Grants attach to the *responsible process's code
   identity*. Children inherit the daemon's responsibility, so one grant on
   `mcremote` covers every spawned agent (claude/goose/opencode/codex) —
   and a grant on Terminal.app covers **nothing** for the LaunchAgent.
2. **Headless means silent.** A LaunchAgent gets no reliable consent
   prompt; denial surfaces as `EPERM` ("operation not permitted"), which
   is why the daemon probes at startup (0069 D5) instead of waiting.
3. **Unsigned identity churns.** The Go linker ad-hoc signs every build
   with a fresh CDHash (identifier `a.out`), so a grant made today stops
   matching after the next real upgrade — usually **without** a new
   prompt; the stale rows just fail. Signing with a real certificate
   (below) is what makes grants durable.

## Granting Full Disk Access

`curl …/install.sh | sh` (and `make install`) place `~/.local/bin/mcremote`.
Neither grants Full Disk Access — TCC has no programmatic grant API. Do this
after the one-liner, then restart the LaunchAgent.

1. System Settings → Privacy & Security → **Full Disk Access**.
2. "+" → press ⌘⇧G in the file dialog → enter the binary path
   (default `~/.local/bin/mcremote`) → Open → toggle on (admin auth).
3. Restart the service:
   `launchctl kickstart -k gui/$(id -u)/com.magiccliremote.mcremote`
4. Verify from the service's own identity: the daemon startup log
   (`~/Library/Logs/mcremote/mcremote.err.log`) — a
   "Full Disk Access not granted" warn line means it did not take.
   `mcremote doctor` from a terminal probes the *terminal's* identity, not
   the LaunchAgent's — useful, but the startup log is authoritative.

## Keeping the grant across upgrades: signed builds

Build with a certificate identity (a free Apple Development certificate
works; sign into Xcode → Settings → Accounts → Manage Certificates if
`security find-identity -v -p codesigning` lists none):

```sh
make install MC_CODESIGN_IDENTITY="Apple Development: you@example.com (TEAMID)"
```

This signs `mcremote`/`mcrelay` with stable identifiers
(`com.magiccliremote.mcremote`) and an anchor-based designated
requirement — the grant then survives rebuilds and updates.
`install-binary.sh` verifies the signature survived the swap. Unsigned
builds keep working; they just re-ask for FDA after every upgrade.

## Recovery and diagnosis

- **Grant present but not working** (typical after upgrading an unsigned
  build): remove and re-add the binary in the FDA pane, or clear stale
  rows first:

  ```sh
  tccutil reset SystemPolicyAllFiles
  ```

  (`tccutil` can only reset — nothing can grant programmatically, and no
  API exists to query the state.)
- **Confirm a live denial is TCC** (and not the codex sandbox):

  ```sh
  log stream --predicate 'subsystem == "com.apple.TCC"' | grep -i deny
  ```

  then reproduce the failure; a `TCC deny` line naming
  `kTCCServiceSystemPolicy…` is the confirmation.
- **The startup probe is a lower bound.** It checks `~/Downloads`;
  iCloud Drive and external volumes carry separate protections that can
  still deny after the probe passes.
- **Grok/ACP note.** For grok sessions the *daemon itself* executes the
  agent's file reads/writes and shell commands, so TCC applies to the
  daemon's identity directly — there is no agent-side grant that can
  substitute.

## What deliberately does not exist

Per 0069's rejected alternatives: no LaunchDaemon (root can never be
prompted at all), no app-bundle wrapper, no PPPC/MDM requirement (profiles
need user-approved MDM enrollment), and no attempt to auto-request access
(no such API exists — claims of an FDA entitlement are folklore).
