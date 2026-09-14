# MADR 0058 — Implementation plan: macOS launchd service (user LaunchAgent)

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to [MADR 0058](./0058-MADR-macos-launchd-service-hardening.md).
Read that first for research, key catalog, and anti-patterns. This document is
the **build order**: phase-sequenced, file-specific, and grounded in the tree.

- **Status:** Implemented (agent-only; phases 0–6 complete)
- **Date:** 2026-07-30
- **Scope:** `mcremote` and `mcrelay` service install on **darwin** as a
  **user LaunchAgent only** (no root, no sudo): `setup-service` /
  `--setup-service` / `--remove` / `--print-only`, launchd plists,
  `scripts/install-binary.sh` service bounce, README / config docs, tests.
  Linux systemd path must keep working unchanged.
- **Out of scope:** LaunchDaemon / system-domain install, logout linger that
  requires sudo, SMAppService, notarization. See §9.
- **Standards:** [Go CLI](standards/go/cli.md), [Go config](standards/go/config.md),
  [Go logging](standards/go/logging.md), project AGENTS.md pre-add gates
- **Related:** [MADR 0019](0019-MADR-opencode-process-management-plan.md)
  (process-group kill), [MADR 0015](0015-MADR-mcrelay-transport-security.md)
  (mcrelay public edge)

---

## 0. MADR 0058 assessment (grounded in code)

### 0.1 Overall judgment

MADR 0058 is **accurate** as a research and design reference for **session
LaunchAgents** and modern `launchctl`. Independent re-check against the current
tree confirms every major code gap it lists for the agent path.

**Product constraint (owner, 2026-07-30):** logout linger is **out of scope**
if it requires sudo. The only robust macOS equivalent of `loginctl enable-linger`
is a LaunchDaemon under `/Library/LaunchDaemons` (root install). Therefore:

- **In scope:** hardened **user LaunchAgent** (`~/Library/LaunchAgents`, domain
  `gui/$UID`), zero privilege escalation.
- **Out of scope:** LaunchDaemon, system domain, `UserName` daemon plists, sudo
  re-exec for install.
- **Document only:** agents stop on logout; keep a login session (or auto-login)
  for always-on Macs; do not claim Linux linger parity on macOS.

### 0.2 MADR claims vs tree facts

| MADR claim | Verified? | Evidence |
|---|---|---|
| `setup-service` Linux-only | **Yes** | `preflight()` in `internal/cli/service/setup.go`: `runtime.GOOS != "linux"` → error |
| Single shared package for both products | **Yes** | `service.Options.Product` (`mcremote` \| `mcrelay`); mcrelay sets `Product: "mcrelay"` in `internal/relay/cli.go`; mcremote leaves default |
| Rich Linux unit template | **Yes** | `mcremote.user.service.tmpl` embed; Restart=always, KillMode=control-group, hardening subset, XDG env |
| Config ensure + bake `--config` | **Yes** | `ensureDefaultConfig` + `render` in `setup.go` |
| Atomic unit write + 0600 when secrets | **Yes** | `writeUnitAtomic`; mode 0600 if `len(ExtraEnviron) > 0` |
| Control-char injection rejected | **Yes** | `normalize` + `TestRenderUnitRejectsControlChars` |
| Reject go-build ephemeral binary | **Yes** | `isEphemeralBuildPath` + `TestSetupRejectsGoRunBinary` |
| Idempotent identical re-run | **Yes** | `AlreadyExisted` + `Unchanged` |
| Remove leaves binary/config | **Yes** | `Remove` only unit + wants symlink |
| CLI dual entry (root flag + subcmd) | **Yes** | `internal/cli/root.go` + `setup_service.go`; mcrelay mirrors in `relay/cli.go` |
| Success text hardcodes systemctl/journalctl | **Yes** | `runSetupService` in both CLIs — **must become platform-aware** |
| Flag help hardcodes systemd | **Yes** | `--unit-name`, `--no-enable`, `--no-linger` descriptions |
| Sample launchd plist only mcremote | **Yes** | `deploy/launchd/com.magiccliremote.mcremote.plist` only |
| Sample uses `/tmp` logs + `launchctl load` | **Yes** | lines 7–8, 40–43 of sample plist |
| `make install` → `~/.local/bin` on darwin | **Yes** | `Makefile` `GOOS=darwin` → `USER_BIN_DIR=$(HOME)/.local/bin` |
| `install-binary.sh` only systemctl bounce | **Yes** | `systemctl_user`; on macOS no-ops service stop/start (binary swap still works) |
| Daemon already SIGTERM-friendly | **Yes** | `internal/daemon/daemon.go` gracefulDrain on cancel / SIGTERM path |
| No SMAppService / app bundle | **Yes** | N/A in tree; MADR D11 correct to skip |

### 0.3 What the MADR got right (keep)

- Agent vs daemon distinction and SIP path rules (daemon knowledge remains for docs)
- Modern `launchctl` verbs (`bootstrap` / `bootout` / `enable` / `kickstart`)
- Default continuous policy: KeepAlive + RunAtLoad + ThrottleInterval=2 + ExitTimeOut=45
- No `AbandonProcessGroup` (parity with `KillMode=control-group`)
- Logs under `~/Library/Logs/<product>/`, not `/tmp`
- `ProcessType=Standard` + NOFILE 65536
- Label scheme `com.magiccliremote.<product>`
- SMAppService out of scope for CLI install
- Shared normalize/config/binary checks across platforms
- Honest “no agent linger” note (now a hard product boundary, not a future phase)

### 0.4 MADR gaps this plan fills

| Gap | Plan resolution |
|---|---|
| **G1** No automated darwin install | Implement agent-only `Setup`/`Remove` |
| **G2** Sample plist incomplete | Harden keys; add mcrelay agent example |
| **G3** No `install-binary.sh` darwin path | Restart LaunchAgent via `gui/$UID` kickstart (no sudo) |
| **G4** CLI/output still Linux-shaped | Platform-aware flags, help, success messages for mcremote **and** mcrelay |
| **G5** No package/file split guidance | Explicit file map; runtime `GOOS` dispatch preferred |
| **G6** Unit-name vs Label mismatch | Map bare names → reverse-DNS Labels |
| **G7** Tests only cover systemd text | Golden plist tests + mocked launchctl |
| **G8** `--no-linger` meaning on macOS | No-op for install (agent never lingers); print short note that macOS has no user-level linger |

### 0.5 Decisions for implementation

| ID | Decision |
|---|---|
| **I1** | **Agent only** on darwin: `~/Library/LaunchAgents/<Label>.plist`, domain `gui/$(id -u)`. Never write `/Library/LaunchDaemons`. Never require root/sudo. |
| **I2** | **No logout linger on macOS.** `NoLinger` / `--no-linger` does not change install scope. On success output: if `!NoLinger`, print that macOS cannot enable linger without a system daemon (unsupported); Linux path unchanged (`loginctl`). |
| **I3** | Labels: `com.magiccliremote.mcremote` / `com.magiccliremote.mcrelay`. Map bare `--unit-name mcremote` → product Label for UX parity with Linux defaults. |
| **I4** | Shared API: `service.Setup` / `Remove` / `RenderUnit` + `RenderPlist` (or unified render). Dispatch on `runtime.GOOS`. |
| **I5** | `Result` gains: `Label`, `Domain`, `LogDir` (and `Scope` fixed to `launchd-agent` on darwin / `systemd-user` on linux) for CLI ops lines. |
| **I6** | Plist keys per MADR §6 agent contract: KeepAlive, RunAtLoad, ThrottleInterval=2, ExitTimeOut=45, ProcessType=Standard, NOFILE 65536, logs under `Library/Logs`, full PATH. |
| **I7** | `install-binary.sh` on darwin: bootout/kickstart **agent** labels only (user domain). No sudo paths. |
| **I8** | No SMAppService, no notarization, no `/System` paths. |
| **I9** | Deploy examples: **agent plists only** (mcremote + mcrelay). No checked-in daemon plists in this work. |
| **I10** | Document always-on Macs: stay logged in / auto-login; do not implement privileged headless install. |

### 0.6 What remains solid (do not regress)

- Linux unit content, tests, and linger via `loginctl`
- Config ensure 0600 / dir 0700
- Binary never copied by setup-service
- go-build path rejection
- Injection rejection on free-text fields
- mcrelay public-port `setcap` note on Linux success path
- Daemon SIGTERM graceful drain behavior

---

## 1. Product behavior (operator-facing)

### 1.1 Command matrix (both products)

| Command | Linux (unchanged) | macOS (this plan) |
|---|---|---|
| `setup-service` | write user unit, enable, restart, linger | write `~/Library/LaunchAgents/<Label>.plist`, enable, bootstrap `gui/$UID`, kickstart |
| `setup-service --print-only` | print unit | print agent plist |
| `setup-service --force` | overwrite unit | overwrite plist + re-bootstrap |
| `setup-service --remove` | stop/disable/rm unit | bootout + disable + rm agent plist |
| `setup-service --no-enable` | skip enable | skip `launchctl enable` |
| `setup-service --no-start` | skip restart | skip bootstrap/kickstart (file only) |
| `setup-service --no-linger` | skip loginctl | **no-op for install** (already no linger); optional one-line note |
| Root flag `--setup-service` | same as subcommand | same |

### 1.2 Session lifetime (honest contract)

```text
Linux:   user unit + loginctl enable-linger  → can survive logout
macOS:   LaunchAgent in gui/$UID              → runs while user GUI session exists;
                                               stops on logout; no sudo linger in product
```

| Operator goal | Support |
|---|---|
| Dev laptop / desktop while logged in | **Yes** — default `setup-service` |
| Survive logout / true headless without login | **Not productized** — document keep-login / auto-login; no sudo daemon path |

### 1.3 Success output (macOS)

```text
ExecStart binary:  /Users/mac/.local/bin/mcremote
Plist:             /Users/mac/Library/LaunchAgents/com.magiccliremote.mcremote.plist
Label:             com.magiccliremote.mcremote
Scope:             launchd-agent (session — stops on logout)
Enabled:           yes (launchctl enable gui/501/…)
Started:           yes (bootstrap + kickstart)
Config:            /Users/mac/.config/mcremote/config.yaml
Logs:              /Users/mac/Library/Logs/mcremote/
Linger:            n/a on macOS (LaunchAgent is session-bound; no sudo system daemon)

Note: setup-service does not install the binary.
      Install/update it with: make install

Status:  launchctl print gui/501/com.magiccliremote.mcremote
Logs:    tail -f ~/Library/Logs/mcremote/mcremote.err.log
Stop:    launchctl bootout gui/501/com.magiccliremote.mcremote
Remove:  mcremote setup-service --remove
```

Always note: Background Items UI (macOS 13+).

### 1.4 Failure modes (actionable errors)

| Condition | Error guidance |
|---|---|
| Not darwin/linux | “unsupported OS” for install |
| launchctl missing | install Xcode CLT / fix PATH |
| bootstrap fails while disabled | enable first (code must order enable→bootstrap) |
| plutil lint fail | show plutil output |
| binary missing / go-build | same as Linux make install hint |
| Label already loaded | bootout then bootstrap |
| `gui/$UID` domain unavailable (no Aqua session) | “log in via GUI/Screen Sharing, then re-run; SSH-only without a user GUI session may not load agents” |

---

## 2. Target plist contract (agent only)

### 2.1 Keys

| Key | Value |
|---|---|
| `Label` | `com.magiccliremote.<product>` |
| `ProgramArguments` | `[binary, "serve", …optional flags…]` — same flag set as Linux ExecStart |
| `WorkingDirectory` | absolute home (or `--working-directory`) |
| `EnvironmentVariables` | `HOME`, `USER`, `LOGNAME`, `PATH`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, plus ExtraEnviron |
| `RunAtLoad` | true |
| `KeepAlive` | true |
| `ThrottleInterval` | 2 |
| `ExitTimeOut` | 45 |
| `ProcessType` | `Standard` |
| `SoftResourceLimits.NumberOfFiles` | 65536 |
| `StandardOutPath` / `StandardErrorPath` | `$HOME/Library/Logs/<product>/<product>.{out,err}.log` |
| `AbandonProcessGroup` | **omit** (false) |
| `UserName` / `GroupName` | **omit** (agent runs as owning user) |

PATH construction (parity with Linux `render` extras + Homebrew):

```text
$HOME/.local/bin
$HOME/.grok/bin
$HOME/.opencode/bin
$HOME/go/bin
$HOME/.local/go/bin
$HOME/.local/flutter/bin
/opt/homebrew/bin
/usr/local/bin
/usr/bin
/bin
+ existing PATH entries not already present
```

### 2.2 Install paths and control plane

| Item | Value |
|---|---|
| Path | `~/Library/LaunchAgents/<Label>.plist` |
| Owner | installing user |
| Mode | 0644 / 0600 if secrets in env |
| bootstrap | `launchctl bootstrap gui/<uid> <plist>` |
| enable | `launchctl enable gui/<uid>/<Label>` |
| kickstart | `launchctl kickstart -k gui/<uid>/<Label>` |
| bootout | `launchctl bootout gui/<uid>/<Label>` |

### 2.3 ProgramArguments flag parity with Linux template

```text
<binary> serve
  [--config PATH]
  [--data-dir PATH]
  [--listen-host HOST]
  [--listen-port PORT]
  [--log-level LEVEL]
  [--log-format FORMAT]
```

### 2.4 XML rendering rules

- Proper escaping for `& < > "` in all string values.
- Reject `\n \r \0` in free-text fields (reuse Linux control-char checks).
- No shell wrapping.
- `plutil -lint` when `plutil` exists.

---

## 3. Code architecture

### 3.1 Package layout (recommended)

Keep everything under `internal/cli/service/`.

```text
internal/cli/service/
  setup.go                 # Options, Result, Setup/Remove orchestration, normalize,
                           # ensureDefaultConfig, writeAtomic, xdg helpers
  unit_render.go           # (optional split) RenderUnit + systemd template
  plist_render.go          # RenderPlist XML builder (compiled on all OS for tests)
  mcremote.user.service.tmpl
  defaults_mcremote.yaml
  defaults_mcrelay.yaml
  setup_test.go            # existing + new plist goldens
  setup_launchd_test.go    # launchctl argv stubs (no live bootstrap required)
```

**Preferred:** pure runtime `switch runtime.GOOS` in `Setup`/`Remove`; always
compile `RenderPlist` so Linux CI runs golden tests. Thin `execLaunchctl` used
only on darwin install paths.

### 3.2 Options / Result API

```go
// Result additions (backward compatible zero values):
type Result struct {
    // existing UnitPath, Binary, UnitName, Enabled, Started, LingerEnabled, …
    Label  string // launchd Label or unit basename for display
    Domain string // e.g. "gui/501" or "" on Linux
    LogDir string // macOS log directory; empty on Linux
    Scope  string // "systemd-user" | "launchd-agent"
}
```

`UnitPath` remains the on-disk definition path (unit or plist).  
`LingerEnabled` stays **false** on darwin always.

### 3.3 Label mapping

```go
func launchdLabel(product, unitName string) string {
    // bare "mcremote" / "mcrelay" / empty → com.magiccliremote.<product>
    // if unitName already looks reverse-DNS (contains '.'), validate and use
    // reject invalid characters
}
```

| Product | Default Linux unit | Default Label |
|---|---|---|
| mcremote | `mcremote` | `com.magiccliremote.mcremote` |
| mcrelay | `mcrelay` | `com.magiccliremote.mcrelay` |

### 3.4 Setup orchestration (pseudo)

```text
func Setup(opts):
  opts = normalize(opts)           // shared
  ensure config if !PrintOnly      // shared
  body = renderForOS(opts)         // unit or agent plist
  Result.UnitBody = body
  if PrintOnly: return

  switch GOOS:
    linux:  return setupSystemd(opts, body)   // existing, incl. linger
    darwin: return setupLaunchdAgent(opts, body)
    else:   error unsupported for install
```

```text
func setupLaunchdAgent(opts, body):
  label = launchdLabel(...)
  logDir = ~/Library/Logs/<product>
  mkdir logDir 0700
  path = ~/Library/LaunchAgents/<label>.plist
  mkdir LaunchAgents 0755
  write atomic body (0644 or 0600)
  plutil -lint path
  uid = current
  bootout gui/uid/label (ignore err)
  if !NoEnable: enable gui/uid/label
  if !NoStart:
    bootstrap gui/uid path
    kickstart -k gui/uid/label
  Result.Scope = "launchd-agent"
  Result.Label, Domain, LogDir, UnitPath set
  Result.LingerEnabled = false
```

```text
func Remove on darwin:
  label = ...
  bootout gui/uid/label (tolerate)
  disable gui/uid/label (tolerate)
  rm ~/Library/LaunchAgents/label.plist (tolerate missing)
  never delete binary, config, logs
  // Do not touch /Library/LaunchDaemons (unsupported / out of scope)
```

### 3.5 CLI layer changes

| File | Change |
|---|---|
| `internal/cli/setup_service.go` | Platform-aware Short/Long/Example; success lines use `res.Scope` / launchctl hints on darwin; set `Product: "mcremote"` explicitly |
| `internal/cli/root.go` | `--setup-service` help string platform-neutral |
| `internal/cli/examples.go` | Add macOS examples (no sudo) |
| `internal/relay/cli.go` | Mirror mcremote output/help; keep Linux setcap note; macOS port note without setcap |
| Flag `--unit-name` help | “service name (Linux unit / macOS maps to Label)” |
| `--no-linger` help | “Linux: skip loginctl enable-linger. macOS: no effect (agents are session-bound)” |

### 3.6 `scripts/install-binary.sh`

Today: only `systemctl --user`. Extend for darwin **agent only**:

```bash
# Map unit arg "mcremote" → label com.magiccliremote.mcremote
# If ~/Library/LaunchAgents/$label.plist exists:
#   launchctl bootout gui/$(id -u)/$label 2>/dev/null || true
#   # after binary swap:
#   launchctl bootstrap gui/$(id -u) "$plist" 2>/dev/null || true
#   launchctl kickstart -k gui/$(id -u)/$label 2>/dev/null || true
# Never call sudo. Never touch /Library/LaunchDaemons.
```

Linux path unchanged.

### 3.7 Deploy assets

| File | Action |
|---|---|
| `deploy/launchd/com.magiccliremote.mcremote.plist` | Rewrite hardened agent example; modern install comments |
| `deploy/launchd/com.magiccliremote.mcrelay.plist` | **Create** agent example |
| Daemon example plists | **Do not add** in this work |
| `deploy/systemd/*` | Unchanged |

Comments: prefer `setup-service`; examples are for review/manual.

---

## 4. File-level work breakdown

### Phase 0 — Prep / docs lock

| Task | Detail | Done when |
|---|---|---|
| 0.1 | This plan (agent-only; linger-if-sudo out) | File in `docs/` |
| 0.2 | MADR 0058 decisions align with I1–I10 | No contradiction |
| 0.3 | Review checkpoint | Owner approves |

**No Go changes.**

### Phase 1 — Plist render + golden tests (no launchctl yet)

| Task | Files | Detail |
|---|---|---|
| 1.1 | `plist_render.go` | `RenderPlist(opts) (string, error)`; XML; all keys §2 |
| 1.2 | `setup.go` | Shared PATH builder; Label helper |
| 1.3 | tests | Goldens: Label, KeepAlive, ThrottleInterval 2, ExitTimeOut 45, logs path, PATH, config flag, control-char reject, escaping |
| 1.4 | `RenderUnit` | No regression |

**Gate:** `go test ./internal/cli/service/ ./internal/cli/` green on Linux CI.

### Phase 2 — Darwin Setup/Remove (agent only)

| Task | Files | Detail |
|---|---|---|
| 2.1 | `setup.go` | `preflight` branch; `Setup`/`Remove` dispatch |
| 2.2 | agent install | §3.4 |
| 2.3 | plutil lint | fail if plutil present and lint fails |
| 2.4 | Result fields | Label, Domain, LogDir, Scope |
| 2.5 | Tests with fake launchctl | assert enable → bootstrap → kickstart order |

**Gate:** stub tests; manual Mac smoke (§6.3).

### Phase 3 — CLI UX (mcremote + mcrelay)

| Task | Files | Detail |
|---|---|---|
| 3.1 | `setup_service.go` | help, success, errors platform-aware |
| 3.2 | `root.go` | root flag help |
| 3.3 | `examples.go` | macOS examples |
| 3.4 | `relay/cli.go` | parity |
| 3.5 | CLI tests | print-only; platform-appropriate strings |

### Phase 4 — Deploy examples + install-binary

| Task | Files | Detail |
|---|---|---|
| 4.1 | `deploy/launchd/*` | agent plists only |
| 4.2 | `scripts/install-binary.sh` | darwin agent bounce |
| 4.3 | `Makefile` comments | launchd labels |
| 4.4 | Manual: `make install` does not strand agent | Mac |

### Phase 5 — Documentation

| Task | Files | Detail |
|---|---|---|
| 5.1 | `README.md` | macOS first-class setup; session-bound; no sudo; Background Items |
| 5.2 | `docs/config.md` | setup-service macOS matrix |
| 5.3 | `docs/config-mcrelay.md` | same |
| 5.4 | `docs/ops-mcrelay.md` | macOS subsection if needed |
| 5.5 | MADR status → implemented when phases 1–5 done | |

### Phase 6 — Polish (optional, same release if time)

| Task | Detail |
|---|---|
| 6.1 | Surface `launchctl print` last exit if non-zero after start |
| 6.2 | Mention System Settings Background Items if bootstrap fails after disable |
| 6.3 | Note cleanup of old `/tmp` sample logs if present |

---

## 5. Detailed algorithms

### 5.1 Agent install (success path)

```text
label = launchdLabel(...)
plistDir = $HOME/Library/LaunchAgents
logDir = $HOME/Library/Logs/<product>
mkdir -p plistDir (0755), logDir (0700)
path = plistDir/label.plist
write atomic body (0644 or 0600)
plutil -lint path
uid = id -u
run launchctl bootout gui/uid/label || true
if !NoEnable: run launchctl enable gui/uid/label
if !NoStart:
  run launchctl bootstrap gui/uid path
  run launchctl kickstart -k gui/uid/label
```

### 5.2 Remove (darwin)

```text
label = ...
bootout gui/uid/label; disable gui/uid/label
rm ~/Library/LaunchAgents/label.plist if present
// leave binary, config, logs
// never touch system LaunchDaemons
```

### 5.3 Print-only

```text
body = RenderPlist(opts)  // agent only
// Binary may be missing (same as RenderUnit preview)
```

### 5.4 Linux linger (unchanged)

```text
if !NoLinger: loginctl enable-linger $USER  // non-fatal today
// darwin never sets LingerEnabled
```

---

## 6. Test plan

### 6.1 Automated (all CI)

| Test | Asserts |
|---|---|
| `TestRenderPlistMinimal` | Label, ProgramArguments[0], serve, RunAtLoad, KeepAlive |
| `TestRenderPlistHardeningKeys` | ThrottleInterval 2, ExitTimeOut 45, ProcessType Standard, NumberOfFiles 65536 |
| `TestRenderPlistLogsNotTmp` | `Library/Logs`, not `/tmp` |
| `TestRenderPlistPATH` | `.local/bin`, `/opt/homebrew/bin` |
| `TestRenderPlistConfigArg` | `--config` when ConfigPath set |
| `TestRenderPlistNoUserName` | no UserName/GroupName keys (agent) |
| `TestRenderPlistEscaping` | `&` / `<` escaped |
| `TestRenderPlistControlChars` | rejected |
| `TestLaunchdLabelMapping` | mcremote → com.magiccliremote.mcremote |
| Existing Linux tests | unchanged green |

### 6.2 Automated with exec stub

| Test | Asserts |
|---|---|
| `TestDarwinSetupAgentOrder` | enable before bootstrap; kickstart present |
| `TestDarwinSetupNeverTouchesSystemDomain` | no `bootstrap system` / `/Library/LaunchDaemons` |
| `TestDarwinRemoveAgent` | bootout + rm agent path only |

### 6.3 Manual Mac checklist (acceptance)

1. `make install`
2. `mcremote setup-service --force` (no sudo)
3. `launchctl print gui/$(id -u)/com.magiccliremote.mcremote` shows running
4. Health/pair still works as expected
5. Logs under `~/Library/Logs/mcremote/`
6. `mcremote setup-service --remove` cleans agent
7. Repeat for `mcrelay`
8. `make install` while agent running restores job (kickstart)
9. Confirm logout stops the agent (document expected behavior)

**Negative:**

1. go-run binary rejected
2. Disabled Background Item → useful error / enable hint
3. `--print-only` on Linux still prints **unit**, not plist; on darwin prints plist

### 6.4 Pre-add / pre-commit

`make pre-add-check` on touched Go packages; `go test ./internal/cli/...`; full
`go test ./...` before commit.

---

## 7. Documentation deliverables (copy outline)

```markdown
## Run as a service

### Linux (systemd --user)
… existing, including linger …

### macOS (launchd user agent)

mcremote setup-service --force
# no sudo; runs while you are logged in

Logs: ~/Library/Logs/mcremote/
Status: launchctl print gui/$(id -u)/com.magiccliremote.mcremote
Remove: mcremote setup-service --remove

Note: macOS LaunchAgents stop when you log out. There is no user-level
equivalent of Linux linger without a system LaunchDaemon (not supported
by setup-service). For always-on Macs, keep a login session (auto-login
or Screen Sharing session).
```

---

## 8. Risk register

| Risk | Impact | Mitigation |
|---|---|---|
| No logout linger on macOS | Headless Mac mini without login won’t run agent | Document; out of scope by design |
| `gui/` domain missing over bare SSH | bootstrap fails | Clear error: use GUI login / Screen Sharing |
| Background Items disable | Silent stop | document System Settings; enable before bootstrap |
| install-binary kickstart fails | Binary updated, job stale | print manual kickstart; still better than today |
| plutil/XML bugs | Job won’t load | golden tests + plutil lint |
| Label vs old unit-name | Confusion | mapping helper + docs |
| mcrelay port &lt;1024 as user | bind fails | port warning (no setcap on macOS) |

---

## 9. Out of scope (explicit)

- **LaunchDaemon / `/Library/LaunchDaemons` / `bootstrap system`**
- **Any install path that requires sudo/root**
- **Logout linger / headless-without-login productization**
- SMAppService / app bundle / notarization / Sparkle
- MDM fleet push of agents
- `AssociatedBundleIdentifiers`
- Changing Linux unit hardening
- Auto-login configuration of macOS
- Windows service manager
- launchd Socket activation / on-demand XPC rewrite

---

## 10. Implementation order (PR stack suggestion)

| PR | Phases | Merge criteria |
|---|---|---|
| **PR1** | 0 + 1 | RenderPlist + tests; MADR align; Linux setup unchanged |
| **PR2** | 2 | Setup/Remove agent on darwin; stubs |
| **PR3** | 3 | CLI help/output both products |
| **PR4** | 4 | deploy agent plists + install-binary.sh |
| **PR5** | 5 (+6 if small) | README/config docs |

Each PR: gofmt/golint/govulncheck clean; `go test ./...`; no Linux unit regressions.

---

## 11. Effort sketch (planning only)

| Phase | Size | Notes |
|---|---|---|
| 0 | XS | docs |
| 1 | M | pure Go XML + tests |
| 2 | M | launchctl agent orchestration (no sudo) |
| 3 | S–M | two CLI surfaces |
| 4 | S | shell + examples |
| 5 | S | docs |
| 6 | XS | polish |

Rough total: **1.5–3 focused implementation days** after plan approval, plus Mac
manual QA.

---

## 12. Acceptance criteria (release “done”)

1. On macOS, `mcremote setup-service --force` installs a hardened LaunchAgent
   and starts it via modern launchctl **without sudo**.
2. Same for `mcrelay`.
3. `--remove` cleans the agent; leaves binary and config.
4. `--print-only` prints valid agent plist XML without installing.
5. Linux `setup-service` behavior and tests unchanged (including linger).
6. No code path writes `/Library/LaunchDaemons` or runs `bootstrap system`.
7. Sample deploy plists match the renderer; no `/tmp` logs; no `launchctl load`.
8. `make install` on macOS best-effort restarts a running agent (no sudo).
9. README documents session-bound lifetime and the intentional linger gap.
10. MADR 0058 status updated to implemented.

---

## 13. Resolved product questions

| # | Question | Resolution |
|---|---|---|
| Q1 | Default agent vs sudo daemon linger? | **Agent only.** Linger-if-sudo **out of scope** (owner 2026-07-30). |
| Q2 | Sudo re-exec? | **No** — not needed. |
| Q3 | Keep `--unit-name` on darwin? | Map bare names to reverse-DNS Labels (I3). |
| Q4 | `--scope` flag? | **Not needed** (single scope). |
| Q5 | Live darwin CI? | Optional; do not block on missing macOS runners. |

---

## 14. Code anchors (baseline reference)

| Area | Path |
|---|---|
| Setup core | `internal/cli/service/setup.go` (~737 lines) |
| Linux unit template | `internal/cli/service/mcremote.user.service.tmpl` |
| Defaults | `defaults_mcremote.yaml`, `defaults_mcrelay.yaml` |
| Tests | `internal/cli/service/setup_test.go` |
| mcremote CLI | `internal/cli/setup_service.go`, `root.go`, `examples.go` |
| mcrelay CLI | `internal/relay/cli.go` (`setupServiceFlags`, `runSetupService`) |
| Binary install | `scripts/install-binary.sh`, `Makefile` `install` target |
| Sample plist | `deploy/launchd/com.magiccliremote.mcremote.plist` |
| Design research | `docs/0058-MADR-macos-launchd-service-hardening.md` |

---

## 15. Status

**Implemented** (agent-only scope, no sudo linger). I1–I10 and §13 remain the
product constraints for future work.
