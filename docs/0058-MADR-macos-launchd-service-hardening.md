# MADR 0058: macOS launchd service setup — research, gaps, and hardened design

- **Status**: Accepted (implemented — agent-only launchd path; see companion plan)
- **Date**: 2026-07-30
- **Deciders**: Project Owner
- **Context**: Make `mcremote` / `mcrelay` macOS service install fully robust, hardened, and consistent with the Linux `setup-service` path. This document records a deep dive of current Apple launchd documentation, multi-source operational practice, a gap analysis of our tree, and a concrete target design.
- **Scope**: **User LaunchAgents only** (`~/Library/LaunchAgents`, no root/sudo). Not LaunchDaemon linger, not App Store packaging / `SMAppService` app bundles.
- **Implementation plan**: [0058-PLAN-macos-launchd-service-implementation.md](0058-PLAN-macos-launchd-service-implementation.md) — build order and file-level PRs. Plan decisions **I1–I10** (agent-only; no sudo linger) refine this MADR for implementation.

**Companions**

| Doc | Role |
|-----|------|
| [0058-PLAN-macos-launchd-service-implementation.md](0058-PLAN-macos-launchd-service-implementation.md) | **Actionable implementation plan** (user LaunchAgent only) |
| [0012-MADR-mcremote-daemon-assessment-action-plan.md](0012-MADR-mcremote-daemon-assessment-action-plan.md) | Daemon lifecycle expectations (SIGTERM, child reaping) |
| [0015-MADR-mcrelay-transport-security.md](0015-MADR-mcrelay-transport-security.md) | mcrelay threat model |
| [0019-MADR-opencode-process-management-plan.md](0019-MADR-opencode-process-management-plan.md) | Child process group / kill semantics (systemd `KillMode=control-group`) |
| [config.md](config.md) / [config-mcrelay.md](config-mcrelay.md) | Config paths, flags, env |
| [deploy/launchd/com.magiccliremote.mcremote.plist](../deploy/launchd/com.magiccliremote.mcremote.plist) | Current example agent (manual) |
| [internal/cli/service/setup.go](../internal/cli/service/setup.go) | Linux `setup-service` implementation |
| [internal/cli/service/mcremote.user.service.tmpl](../internal/cli/service/mcremote.user.service.tmpl) | Linux unit template (parity reference) |

---

## 1. Problem statement

Linux operators get a first-class path:

```text
mcremote setup-service --force
mcrelay  setup-service --force
```

That path writes a hardened **user** unit, ensures config, enables, starts, and optionally enables linger. On macOS today the story is:

1. `setup-service` **refuses** non-Linux (`preflight` → “requires Linux”).
2. Operators must hand-edit and `launchctl load` a sample plist.
3. The sample is incomplete relative to the Linux unit (PATH, logs, throttle, exit timeout, config bake-in, remove/upgrade flow).
4. Docs still teach the **legacy** `launchctl load` / `unload` verbs that Apple has marked legacy since OS X 10.10 and that community + admin tooling have replaced with domain-aware `bootstrap` / `bootout` / `enable` / `disable`.

We need a documented, accurate target so a future macOS `setup-service` (and the checked-in plists) is correct on current macOS idioms (Ventura–Tahoe era), not a 2012 copy-paste.

---

## 2. Research method

Sources consulted (2026-07-30):

| Class | Sources |
|-------|---------|
| **Apple official / archive** | [Creating Launch Daemons and Agents](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) (Daemons and Services Programming Guide, last rev. 2016-09-13); [Designing Daemons and Services](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/DesigningDaemons.html); [Service Management — Updating helper executables](https://developer.apple.com/documentation/servicemanagement/updating-helper-executables-from-earlier-versions-of-macos); Apple Support *Script management with launchd* (Terminal User Guide, macOS Tahoe 26); Apple Developer Forums (Service Management tag, SMAppService threads); TN2083 *Daemons and Agents* (still cited by Apple staff as relevant though aged) |
| **Man pages (current Darwin)** | `launchd.plist(5)` (e.g. [keith.github.io mirror](https://keith.github.io/xcode-man-pages/launchd.plist.5.html)); `launchctl(1)` domain model (`system`, `user/<uid>`, `gui/<uid>`) |
| **Deep community primers** | [launchd.info](https://www.launchd.info/) (configuration key catalog); Alan Siu *launchctl “new” subcommand basics* (2023); Babo D *Launchctl 2.0 Syntax* (still the domain/target primer); InventiveHQ *Manage Login Items, LaunchAgents, LaunchDaemons* (updated 2026); Eclectic Light / StackExchange agent-vs-daemon discussions |
| **Operational / product patterns** | Homebrew `brew services` (`bootstrap gui/$UID`, agent under `~/Library/LaunchAgents`); GitLab Runner issue #29324 (migrate load→bootstrap); mise-en-place launchd agents; PaperCut / admin fleet plists (ownership `root:wheel` + `644` for `/Library/*`) |
| **In-repo** | `internal/cli/service/*`, `deploy/launchd/*`, `deploy/systemd/*`, README setup-service section |

Primary authorities for **keys and process expectations**: `launchd.plist(5)` and the Apple guide’s “MUST NOT daemonize / SHOULD handle SIGTERM” rules. Primary authority for **modern control plane**: `launchctl` non-legacy subcommands + domain targets.

---

## 3. launchd model (what still holds in 2026)

### 3.1 Agents vs daemons

| | Launch **Agent** | Launch **Daemon** |
|--|------------------|-------------------|
| Paths | `~/Library/LaunchAgents`, `/Library/LaunchAgents` | `/Library/LaunchDaemons` |
| Identity | Logged-in user (Aqua / user session) | root by default, or `UserName` / `GroupName` |
| Lifetime | Tied to user login session; torn down on logout | Boot → shutdown; no GUI session |
| GUI / WindowServer | Allowed (session-dependent) | **Must not** connect to WindowServer |
| Home / keychain / user CLIs | Natural | Awkward unless `UserName` + carefully set env |
| Privilege | User | System (SIP-protected under `/System`) |

Apple: agents are “specific to a given logged-in user and execute only while that user is logged in.” Daemons are system-context. Never put third-party jobs under `/System/Library/*` (SIP).

**For mcremote / mcrelay product install: LaunchAgent under `~/Library/LaunchAgents` only.**  
Rationale: parity with Linux `systemd --user` *without privilege escalation*, needs `$HOME`, XDG config/data, and the ability to `exec` user-installed CLIs (`grok`, `opencode`, `codex`, `goose`) without root.

**LaunchDaemon (system domain + `UserName`) is out of product scope.** It is the only robust way to survive logout / run with no Aqua login, but it requires root to install under `/Library/LaunchDaemons`. Owner decision: **do not implement any install path that needs sudo.** Always-on Macs should keep a login session (auto-login / Screen Sharing). Research notes on daemons remain below for operators who hand-roll outside `setup-service`.

### 3.2 Process expectations (Apple / man page)

A job launched by launchd **MUST NOT**:

- Call `daemon(3)`.
- `fork` and have the parent exit (double-fork “daemonize”).

A job **SHOULD**:

- Prefer on-demand launch criteria when possible.
- Handle **SIGTERM** (ideally via a dispatch/source-style handler) and exit promptly after unwinding work.

A job **need not** (launchd can do these via plist keys): setuid/setgid, chdir, setsid, close stray FDs, setrlimit, setpriority.

Our Go daemons already treat SIGTERM as graceful shutdown (`internal/daemon`). That is correct for launchd. Do **not** introduce a “detach into background” path for `serve`.

### 3.3 Continuous service vs on-demand

Apple’s guide historically prefers **on-demand** jobs (Sockets / MachServices / events) so launchd owns dependency ordering. For a long-lived WebSocket/TLS daemon that holds sessions and child agent processes, **continuous** operation is the correct product choice—the same reason Linux uses `Restart=always` rather than socket activation.

That maps cleanly to:

- `RunAtLoad` = true (start when the agent domain loads / user logs in)
- `KeepAlive` = true (restart on any exit; mirrors `Restart=always`)

Note from `launchd.plist(5)`:

- `KeepAlive` true **implicitly implies** speculative launch (similar to `RunAtLoad`); stating both is common and fine for clarity.
- Jobs that crash-loop are **throttled** (default **10 s** between spawns). Override with `ThrottleInterval` when a shorter restart is desired (we use 2 s on Linux).
- Deprecated: `OnDemand` false as synonym for KeepAlive; remove if ever present.

Alternative (usually **wrong** for us):  
`KeepAlive` → `{ SuccessfulExit = false }` restarts only on non-zero exit. That is closer to `Restart=on-failure` and would **not** recover a clean but accidental exit. Prefer unconditional KeepAlive for parity with Linux.

### 3.4 Domains and modern `launchctl` (post-legacy)

Legacy (still often work, but discouraged):

```bash
launchctl load   ~/Library/LaunchAgents/com.example.plist
launchctl unload ~/Library/LaunchAgents/com.example.plist
```

Current idioms (macOS 11+ operational consensus; man page domain model):

| Intent | Command pattern |
|--------|-----------------|
| Load agent into GUI login domain | `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/LABEL.plist` |
| Unload agent | `launchctl bootout gui/$(id -u)/LABEL` (or bootout with path form) |
| Enable (override disabled DB) | `launchctl enable gui/$(id -u)/LABEL` |
| Disable | `launchctl disable gui/$(id -u)/LABEL` |
| Restart running job | `launchctl kickstart -k gui/$(id -u)/LABEL` |
| Inspect | `launchctl print gui/$(id -u)/LABEL` |
| System daemon | `sudo launchctl bootstrap system /Library/LaunchDaemons/LABEL.plist` |

Domain notes that matter for us:

| Domain | Meaning |
|--------|---------|
| `gui/<uid>` | User **login** (Aqua) domain; convenient form of user-login. **Default target for GUI-logged-in developers.** Homebrew services use this. |
| `user/<uid>` | User domain that **may exist without** a full GUI login; different bootstrap namespace. Do not assume it is interchangeable with `gui/` for every job. |
| `system` | Privileged system domain (LaunchDaemons). |

**Enable/disable gotcha (widely reproduced):** if a service is **disabled** in the override database, `bootstrap` fails with errors such as “Bootstrap failed: 5: Input/output error”. Always `enable` before `bootstrap` when installing; on remove, `bootout` then `disable` (or leave enabled state alone and delete the plist—document both).

**Plist on disk vs loaded job:** editing a plist does **not** hot-reload keys. Correct upgrade sequence: bootout → rewrite plist → enable → bootstrap (or kickstart after a careful replace where supported). Our Linux path uses `daemon-reload` + `restart`; macOS needs an explicit bootout/bootstrap or kickstart after rewrite.

### 3.5 Session types (`LimitLoadToSessionType`)

Relevant agent session types (agents only):

| Type | Typical use |
|------|-------------|
| `Aqua` | Full GUI login (default for many agents) |
| `Background` | Non-GUI background; can load without full Aqua UI in some contexts |
| `LoginWindow` | Pre-login / loginwindow only |

For mcremote we default to **no** `LimitLoadToSessionType` restriction (broadest user-agent load under `gui/`), unless testing shows we need `Aqua` only. Avoid `LoginWindow`. Do not invent a “System” session type for agents.

### 3.6 Child process groups (`AbandonProcessGroup`)

Default (**false**): when launchd stops a job, it sends SIGTERM and then kills remaining processes in the job’s process group. That is the launchd analogue of systemd **`KillMode=control-group`**.

Setting `AbandonProcessGroup` true lets children survive the parent—**wrong** for mcremote (would leak `opencode` / ACP engines; see MADR 0019).

**Decision:** omit the key (default false). Rely on daemon graceful shutdown first; launchd process-group cleanup is the backstop. Align `ExitTimeOut` with our stop budget (~45 s Linux `TimeoutStopSec`).

### 3.7 Logging

launchd has **no journald**. Production practice:

- Prefer **`StandardOutPath` / `StandardErrorPath`** under the user’s log tree, e.g.  
  `~/Library/Logs/mcremote/mcremote.out.log` and `.err.log`  
  (create directory at install time, mode `0700`).
- **Avoid `/tmp/*.log`**: multi-user readable, sticky-directory races, easy to lose on reboot, and poor hygiene for tokens accidentally logged at debug.
- Console.app / `log stream` still useful for launchd itself (`subsystem == "com.apple.xpc.launchd"`), not a substitute for app stdout when using file redirection.
- Daemon should continue using structured `log/slog` to stderr; launchd captures it via `StandardErrorPath`.

### 3.8 Resource classification (`ProcessType`)

From `launchd.plist(5)`:

| Value | Effect |
|-------|--------|
| (unset) / `Standard` | Light default limits |
| `Background` | Stronger CPU/I/O throttling so jobs don’t disrupt interactive UX |
| `Adaptive` / `Interactive` | Higher priority classes for user-facing or adaptive work |

**Decision:** `ProcessType` = **`Standard`**.  
`Background` would throttle a latency-sensitive control plane and child coding agents. We are not a batch maintenance script.

Also set:

```xml
<key>SoftResourceLimits</key>
<dict>
  <key>NumberOfFiles</key>
  <integer>65536</integer>
</dict>
```

Parity with Linux `LimitNOFILE=65536`. Do **not** raise hard system-wide limits from a user agent.

### 3.9 Security surface that launchd *does not* give us

Linux user units use `NoNewPrivileges`, `PrivateTmp`, `RestrictSUIDSGID`, etc. **launchd has no equivalent sandbox keys** for third-party agents. Hardening on macOS is:

1. **Least privilege of the account** (user agent, not root daemon).
2. **Plist and log permissions** (see below).
3. **No secrets in world-readable plists** (mode `0600` when `EnvironmentVariables` carry secrets; prefer config file `0600` under `~/.config/...` which we already ensure).
4. **Absolute `ProgramArguments[0]`** to a real install path (not `go-build` temp).
5. **Umask** (e.g. `63` decimal = `077` octal, or string `"077"`) so created files default tight—optional; config writer already uses `0600`.
6. **TCC / Full Disk Access**: unsigned CLI tools under `$HOME` can hit silent “Operation not permitted” when touching protected locations. Document operator FDA grant for Terminal/binary if providers need Desktop/Documents, not as a default install step.
7. **Background Items (macOS 13+):** installing a LaunchAgent surfaces a system notification and an entry under **System Settings → General → Login Items & Extensions** (wording varies by OS version). Users can disable the item there—equivalent to `launchctl disable`. Install UX must mention this.
8. **`SMAppService` (macOS 13+):** correct for **app-bundled** agents/daemons registered from a signed `.app`. **Out of scope** for bare Go CLI install to `~/.local/bin`. Do not force App Store Service Management packaging for `setup-service`. Optional later if we ship a Mac app wrapper.
9. **`AssociatedBundleIdentifiers`:** optional plist key to associate a free-standing agent with an app for Login Items UI grouping. Skip until we have a Mac app bundle ID.

### 3.10 Plist file rules

- Filename **must** end in `.plist`.
- Convention: filename = **`Label` + `.plist`** (e.g. `com.magiccliremote.mcremote.plist`).
- Label: reverse-DNS, unique per launchd instance.
- Validate: `plutil -lint path.plist` before bootstrap.
- User LaunchAgent ownership: **installing user**, not world-writable; mode **`0644`** default, **`0600`** if environment carries secrets.
- Global LaunchDaemon ownership: **`root:wheel`**, mode **`0644`** (required or load fails).
- Property list values for `EnvironmentVariables` must be **strings** (other types ignored).
- No shell globbing unless `EnableGlobbing` (we will not enable it).
- XML text must escape `& < >` in paths (Go `encoding/xml` or careful escaping)—paths with `&` are rare but must not produce invalid plists.

### 3.11 Linger / headless gap (product boundary)

Linux `setup-service` runs `loginctl enable-linger` so the user systemd instance survives logout. **There is no first-class “linger” for GUI LaunchAgents without a system LaunchDaemon (sudo).** When the user logs out of Aqua, user agents are SIGTERM’d.

**Product choice:** accept this gap. `setup-service` on macOS never escalates to root and never installs LaunchDaemons.

| Scenario | Recommendation |
|----------|----------------|
| Laptop / desktop, user stays logged in | LaunchAgent via `setup-service` |
| Mac mini “server”, auto-login / always-logged-in | LaunchAgent; keep the session alive |
| True headless, no GUI login | **Not supported** by `setup-service`; keep a login session or hand-roll outside product |
| SSH-only without Aqua | Prefer install from a GUI/Screen Sharing session; `gui/$UID` may be unavailable |

Document in README and setup-service help; do not claim Linux linger parity on macOS.

---

## 4. Current in-repo state (gap analysis)

### 4.1 What exists

| Artifact | Status |
|----------|--------|
| `internal/cli/service/setup.go` | Linux-only preflight; rich unit render, atomic write, enable/start/linger, remove, config ensure, injection hardening |
| `internal/cli/service/mcremote.user.service.tmpl` | Full env + restart + kill mode + partial hardening |
| `deploy/systemd/mcremote.user.service` / `mcrelay.user.service` | Documented examples |
| `deploy/launchd/com.magiccliremote.mcremote.plist` | Minimal example agent only |
| mcrelay launchd example | **Missing** |
| `setup-service` on darwin | **Blocked** (`requires Linux`) |

### 4.2 Sample plist review (`deploy/launchd/com.magiccliremote.mcremote.plist`)

| Item | Current | Assessment |
|------|---------|------------|
| Label | `com.magiccliremote.mcremote` | Good reverse-DNS |
| ProgramArguments | `/usr/local/bin/mcremote serve` | OK skeleton; Apple Silicon Homebrew is often `/opt/homebrew/bin`; `make install` uses `~/.local/bin` — sample should not hardcode only `/usr/local` |
| WorkingDirectory / HOME | `/Users/REPLACE_ME` | Manual placeholder; fine for example, bad for “install” |
| PATH | `/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin` | Missing `~/.local/bin`, `~/.grok/bin`, `~/.opencode/bin`, Go bins — **weaker than Linux unit** |
| XDG_* | Set | Good |
| RunAtLoad + KeepAlive | true | Correct for continuous daemon |
| ThrottleInterval | absent | Defaults to **10 s** (vs Linux 2 s) |
| ExitTimeOut | absent | System default; should be **45** for child engine drain |
| ProcessType | absent | Should be `Standard` |
| SoftResourceLimits | absent | Should set `NumberOfFiles` 65536 |
| StandardOut/Err | `/tmp/mcremote.*.log` | **Anti-pattern** (world-readable tmp) |
| Config bake-in | none | Linux setup always ensures + bakes `--config` |
| AbandonProcessGroup | absent | Correct (default false) |
| Install docs | `launchctl load` | **Legacy**; must become bootstrap/enable |
| mcrelay | N/A | Need second label `com.magiccliremote.mcrelay` |

### 4.3 Linux parity matrix (target)

| Capability | Linux `setup-service` | macOS today | Target macOS |
|------------|----------------------|-------------|--------------|
| Write service definition | `~/.config/systemd/user/*.service` | manual plist | `~/Library/LaunchAgents/com.magiccliremote.<product>.plist` |
| Ensure default config | yes | no | yes (same ensureDefaultConfig) |
| Bake `--config` into argv | yes | no | yes |
| Binary resolution | `~/.local/bin` prefer, reject go-build | hardcoded sample | same normalize() rules |
| Enable on login/boot of user session | systemctl enable + linger | load once | `launchctl enable` + plist in LaunchAgents (auto on login) |
| Start / restart | systemctl restart | load | bootstrap + `kickstart -k` |
| Remove | stop/disable/rm/reload | manual | bootout + disable + rm plist |
| Survive logout | linger | **no** | **document only** — no sudo daemon path in product |
| Restart policy | Restart=always, RestartSec=2 | KeepAlive + 10s throttle | KeepAlive + ThrottleInterval=2 |
| Stop timeout | TimeoutStopSec=45 | default | ExitTimeOut=45 |
| Kill children | KillMode=control-group | default process group | keep default; no AbandonProcessGroup |
| Logs | journald | /tmp files | `~/Library/Logs/<product>/` |
| Hardening | systemd sandbox subset | almost none | ProcessType, NOFILE, umask, perms, no secrets in 0644 plist |
| Print-only | yes | no | yes (print plist) |
| Force overwrite | yes | N/A | yes |
| Injection / control-char rejection | yes | N/A | yes (XML-safe + reject `\n\r\0`) |
| Preflight | systemctl + user bus | none | darwin + launchctl + writable LaunchAgents dir |

---

## 5. Decisions

| ID | Topic | Decision |
|----|-------|----------|
| **D1** | Service type | **User LaunchAgent only** in `~/Library/LaunchAgents` for mcremote and mcrelay. No system LaunchDaemon install in product. |
| **D2** | Label scheme | `com.magiccliremote.mcremote` and `com.magiccliremote.mcrelay` (filename = label + `.plist`). Bare Linux-style names map to these Labels. |
| **D3** | launchctl API | **Modern only:** `enable` / `bootstrap` / `bootout` / `kickstart` / `print`. Domain **`gui/$(id -u)`** only. Never teach `load`/`unload` in new docs. Never automate `bootstrap system`. |
| **D4** | Keepalive | `KeepAlive` = true, `RunAtLoad` = true, `ThrottleInterval` = 2. Not SuccessfulExit-gated. |
| **D5** | Stop / children | `ExitTimeOut` = 45; **do not** set `AbandonProcessGroup`. Job handles SIGTERM; launchd kills stragglers in the process group. |
| **D6** | Logs | `StandardOutPath` / `StandardErrorPath` under `~/Library/Logs/<product>/` (dir `0700`). Never `/tmp`. |
| **D7** | Environment | Mirror Linux template intent: `HOME`, `USER`, `LOGNAME`, `PATH` (user tool prefixes + `/opt/homebrew/bin` + `/usr/local/bin` + system), `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`. Omit fake `XDG_RUNTIME_DIR`. |
| **D8** | Config | Same as Linux: ensure default `~/.config/<product>/config.yaml` (`0600`) and pass `--config` in `ProgramArguments` when installing. |
| **D9** | Secrets in plist | If `--env` injects variables, write plist mode **`0600`**; otherwise **`0644`**. Prefer secrets in config file, not EnvironmentVariables. |
| **D10** | ProcessType / limits | `ProcessType` = `Standard`; `SoftResourceLimits.NumberOfFiles` = 65536. |
| **D11** | SMAppService | **Not** required for CLI `setup-service`. Revisit only if we ship a signed Mac app. |
| **D12** | LaunchDaemon / linger | **Out of product scope** when it requires sudo. Linux linger stays Linux-only. macOS `--no-linger` is a no-op for install. Document session-bound lifetime. |
| **D13** | Logout / headless | Document gap; recommend keep-login / auto-login. No privileged install path. |
| **D14** | Implementation shape | Extend `internal/cli/service` with a darwin agent backend (render plist + launchctl) behind the same `setup-service` CLI; keep Linux path unchanged. Shared: normalize, ensureDefaultConfig, binary checks. |
| **D15** | Checked-in examples | Refresh `deploy/launchd/` with **agent** examples only (mcremote + mcrelay), modern install comments, log paths under `Library/Logs`. |

---

## 6. Target plist contract (reference)

Illustrative mcremote agent (paths filled by setup-service; not hand-edited in production):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.magiccliremote.mcremote</string>

  <key>ProgramArguments</key>
  <array>
    <string>/Users/alice/.local/bin/mcremote</string>
    <string>serve</string>
    <string>--config</string>
    <string>/Users/alice/.config/mcremote/config.yaml</string>
  </array>

  <key>WorkingDirectory</key>
  <string>/Users/alice</string>

  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>/Users/alice</string>
    <key>USER</key>
    <string>alice</string>
    <key>LOGNAME</key>
    <string>alice</string>
    <key>PATH</key>
    <string>/Users/alice/.local/bin:/Users/alice/.grok/bin:/Users/alice/.opencode/bin:/Users/alice/go/bin:/Users/alice/.local/go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
    <key>XDG_CONFIG_HOME</key>
    <string>/Users/alice/.config</string>
    <key>XDG_DATA_HOME</key>
    <string>/Users/alice/.local/share</string>
    <key>XDG_CACHE_HOME</key>
    <string>/Users/alice/.cache</string>
  </dict>

  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>2</integer>
  <key>ExitTimeOut</key>
  <integer>45</integer>

  <key>ProcessType</key>
  <string>Standard</string>
  <key>SoftResourceLimits</key>
  <dict>
    <key>NumberOfFiles</key>
    <integer>65536</integer>
  </dict>

  <key>StandardOutPath</key>
  <string>/Users/alice/Library/Logs/mcremote/mcremote.out.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/alice/Library/Logs/mcremote/mcremote.err.log</string>
</dict>
</plist>
```

Optional argv suffixes (same flags as Linux template): `--data-dir`, `--listen-host`, `--listen-port`, `--log-level`, `--log-format`.

mcrelay: same structure; label `com.magiccliremote.mcrelay`; binary/config/logs under `mcrelay`.

---

## 7. Target install / remove algorithms

### 7.1 `setup-service` (darwin)

```text
1. normalize(opts)          // shared: product, binary, config, reject go-build, abs paths
2. ensureDefaultConfig      // shared
3. renderPlist(opts)        // XML; escape values; reject control chars in free-text fields
4. if PrintOnly → return body
5. preflightDarwin:
     - runtime.GOOS == "darwin"
     - launchctl in PATH
     - home + ~/Library/LaunchAgents creatable
     - optional: id -u works
6. mkdir ~/Library/Logs/<product> (0700)
7. mkdir ~/Library/LaunchAgents (0755)
8. atomic write plist (mode 0644 or 0600 if secret env)
9. plutil -lint if plutil exists (fail install on invalid plist)
10. uid := current uid
    domain service := gui/<uid>/<Label>
11. launchctl bootout gui/<uid>/<Label>   // ignore "no such process"
12. launchctl enable gui/<uid>/<Label>
13. launchctl bootstrap gui/<uid> <plist-path>
14. if start wanted:
      launchctl kickstart -k gui/<uid>/<Label>
15. print success + Background Items note + log paths + session-bound lifetime note
```

On partial failure after write: error must include manual finish commands (same spirit as Linux `manual` string).

### 7.2 `setup-service --remove` (darwin)

```text
1. bootout gui/<uid>/<Label>   // tolerate not loaded
2. disable gui/<uid>/<Label>   // tolerate
3. remove plist if present
4. do NOT delete binary, config, or logs (parity with Linux)
```

### 7.3 Operator manual checklist (until automation lands)

```bash
# install binary
make install   # or copy to ~/.local/bin/mcremote

# config
mkdir -p ~/.config/mcremote && chmod 700 ~/.config/mcremote
# write config.yaml mode 600

# logs
mkdir -p ~/Library/Logs/mcremote && chmod 700 ~/Library/Logs/mcremote

# plist → ~/Library/LaunchAgents/com.magiccliremote.mcremote.plist
plutil -lint ~/Library/LaunchAgents/com.magiccliremote.mcremote.plist

UID_NUM=$(id -u)
LABEL=com.magiccliremote.mcremote
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"

launchctl bootout "gui/${UID_NUM}/${LABEL}" 2>/dev/null || true
launchctl enable "gui/${UID_NUM}/${LABEL}"
launchctl bootstrap "gui/${UID_NUM}" "$PLIST"
launchctl kickstart -k "gui/${UID_NUM}/${LABEL}"

launchctl print "gui/${UID_NUM}/${LABEL}"
```

---

## 8. Hardening checklist (acceptance for “fully robust”)

### Plist / job

- [ ] Label reverse-DNS; filename matches Label
- [ ] Absolute executable path; executable bit set; not under `$TMPDIR/go-build`
- [ ] `ProgramArguments` array (never a shell `-c` string unless unavoidable)
- [ ] `KeepAlive` + `RunAtLoad` + `ThrottleInterval=2` + `ExitTimeOut=45`
- [ ] `ProcessType=Standard` + NOFILE soft limit 65536
- [ ] Logs under `~/Library/Logs/<product>/`, directory `0700`
- [ ] Env PATH includes user CLI tool dirs + Homebrew prefixes
- [ ] Config ensured and passed via `--config`
- [ ] No `AbandonProcessGroup`
- [ ] No `RootDirectory` / chroot
- [ ] No deprecated `OnDemand` / `ServiceIPC`
- [ ] plutil-clean XML

### Control plane

- [ ] bootstrap/bootout/enable/kickstart only (no load/unload in code or new docs)
- [ ] Domain `gui/$UID` for default agent install
- [ ] enable before bootstrap
- [ ] bootout before rewrite on upgrade/`--force`
- [ ] Idempotent re-run when plist byte-identical (still converge enable/start)
- [ ] `--remove` does not delete binary/config
- [ ] Clear errors when not on Aqua session / bootstrap fails

### Security / ops docs

- [ ] Plist `0600` when env may contain secrets
- [ ] Document Background Items notification (macOS 13+)
- [ ] Document no logout linger (no sudo daemon path); keep-login guidance
- [ ] Document FDA/TCC only if operators hit “Operation not permitted”
- [ ] Document log locations and `launchctl print` / Console.app debugging
- [ ] mcrelay low-port warning if applicable (user agent cannot bind &lt;1024 without other help—same class of issue as Linux user units)

### Parity tests (when implemented)

- [ ] Render golden plist tests (paths substituted)
- [ ] Reject control characters / invalid env keys (shared with Linux)
- [ ] `--print-only` works on darwin without launchctl
- [ ] Integration tests gated `//go:build darwin` for bootstrap where CI has macOS runners

---

## 9. Implementation plan

**Superseded for sequencing by**
[0058-PLAN-macos-launchd-service-implementation.md](0058-PLAN-macos-launchd-service-implementation.md).

That plan is **agent-only** (no sudo linger): `RenderPlist`, darwin agent
Setup/Remove, CLI UX, deploy agent plists, `install-binary.sh` bounce, docs,
tests. Summary:

| Phase | Work |
|-------|------|
| **0** | Docs / decision lock (this MADR + plan) |
| **1** | `RenderPlist` + golden tests |
| **2** | darwin `Setup`/`Remove` (LaunchAgent / `gui/$UID` only) |
| **3** | Platform-aware CLI (mcremote + mcrelay) |
| **4** | `deploy/launchd/*` agents + `scripts/install-binary.sh` |
| **5** | README / config docs |
| **6** | Polish |

Out of scope: LaunchDaemon/sudo linger, SMAppService, notarization, MDM fleet push.

---

## 10. Anti-patterns to avoid

1. **`launchctl load -w`** in new code or docs.
2. **Logs in `/tmp`.**
3. **`KeepAlive` + very low `ThrottleInterval` without fixing crash loops** (masks bugs; 2 s is enough for parity, not 0).
4. **`AbandonProcessGroup` true** (orphans agent CLIs).
5. **`ProcessType` Background** for the control plane.
6. **Shell wrapper** as Program without a strong reason (quoting, signal, PATH surprises).
7. **Root LaunchDaemon “for convenience”** while still pointing at a single user’s `~/.config` without `UserName` + permissions review.
8. **Assuming linger** after logout.
9. **Bootstrap without enable** after a user disabled the Background Item.
10. **Editing plist in place and expecting reload** without bootout/bootstrap or kickstart.
11. **Shipping only `/usr/local/bin`** samples on Apple Silicon (Homebrew default prefix is `/opt/homebrew`).
12. **Putting relay registration secrets in a 0644 plist EnvironmentVariables block.**

---

## 11. Comparison: systemd user unit ↔ launchd agent (cheat sheet)

| Concern | systemd --user | launchd agent |
|---------|----------------|---------------|
| Unit file | `~/.config/systemd/user/foo.service` | `~/Library/LaunchAgents/com…foo.plist` |
| Reload definitions | `daemon-reload` | rewrite + bootout/bootstrap |
| Enable | `systemctl --user enable` | `launchctl enable gui/$UID/label` + presence in LaunchAgents |
| Start | `start` / `restart` | `bootstrap` + `kickstart -k` |
| Stop | `stop` | `bootout` |
| Restart policy | `Restart=always` + `RestartSec` | `KeepAlive` + `ThrottleInterval` |
| Stop timeout | `TimeoutStopSec` | `ExitTimeOut` |
| Kill children | `KillMode=control-group` | default process group (not AbandonProcessGroup) |
| Logs | journald | StandardOut/ErrorPath files |
| Linger | `loginctl enable-linger` | **none** (session-bound) |
| Sandbox | many unit directives | minimal; OS TCC + permissions |
| Validate | systemd-analyze verify (optional) | `plutil -lint` |

---

## 12. Consequences

### Positive

- Single mental model for operators: `setup-service` on both platforms **without sudo on macOS**.
- Aligns with current Apple/community launchctl domains (less “Bootstrap failed: 5” mystery).
- Closes security footguns in the sample (`/tmp` logs, weak PATH, missing exit/throttle policy).
- Explicit session-bound lifetime prevents false confidence vs Linux linger.

### Negative / residual risk

- **No logout linger on macOS** by design (would need sudo LaunchDaemon).
- Without SMAppService / notarized app, Background Items UX is “CLI dropped a plist,” which some users disable unknowingly.
- launchd will not gain systemd-style namespace hardening; process isolation stays OS + Go daemon discipline.

### Neutral

- Continuous `KeepAlive` jobs are slightly at odds with Apple’s on-demand ideal; acceptable and industry-standard for personal servers (Homebrew services, sync tools, etc.).

---

## 13. References (stable starting points)

1. Apple — *Creating Launch Daemons and Agents* (BPSystemStartup).  
2. Apple — `launchd.plist(5)` / `launchctl(1)` (Darwin man pages).  
3. Apple Support — *Script management with launchd* (Terminal guide, current macOS).  
4. Apple — Service Management / helper updates (SMAppService context for app-bundled jobs).  
5. [launchd.info](https://www.launchd.info/) — practical key encyclopedia.  
6. Alan Siu — *launchctl “new” subcommand basics for macOS* (bootstrap/bootout/enable).  
7. InventiveHQ — LaunchAgents/Daemons management (2026 operational summary, Background Items).  
8. Homebrew services — de-facto user-agent install pattern (`gui/$UID`).  

---

## 14. Status of code changes

This MADR **records research and locks design**. Implementation is tracked in
[0058-PLAN](0058-PLAN-macos-launchd-service-implementation.md). Update Status to
“Accepted (implemented)” when that plan’s acceptance criteria are met.
