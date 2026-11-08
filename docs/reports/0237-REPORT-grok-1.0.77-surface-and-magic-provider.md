<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Grok Build 1.0.77 surface vs the magic-cli-remote grok provider

Probed 2026-11-07/08. This report observes; it decides nothing. A later
MADR/PLAN would re-pin `KnownGoodVersion`, refresh
`testdata/wire/<version>/`, and choose which surfaces to adopt. The governing
pin is still
[0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](../decisions/0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md)
D1 (`KnownGoodVersion = "1.0.70"`).

Evidence classes, marked as they appear:

* **(probed)** — measured live against the installed `grok` binary
  (`~/.local/bin/grok` → `~/.grok/bin/grok`).
  `grok version --json` → `{"currentVersion":"1.0.77 (50ca579fa996)","channel":"stable"}`.
  `grok update --check --json` → `latestVersion=1.0.77`, `updateAvailable=false`,
  `installer=npm`. ACP initialize was driven twice: Content-Length JSON-RPC,
  then newline-delimited JSON-RPC, against
  `grok --no-auto-update agent --no-leader stdio` in a scratch cwd. No
  `session/prompt` was sent. The process inherited the operator grok home, so
  `cached_token` and the live model catalog reflect that home, not a cold
  isolated install. Host names and absolute homes are redacted below.
* **(docs)** — official pages fetched 2026-11-08:
  [docs.x.ai/build/overview](https://docs.x.ai/build/overview),
  [headless-scripting](https://docs.x.ai/build/cli/headless-scripting),
  [CLI reference](https://docs.x.ai/build/cli/reference),
  [modes and commands](https://docs.x.ai/build/modes-and-commands),
  [MCP servers](https://docs.x.ai/build/features/mcp-servers),
  [skills/plugins/marketplaces](https://docs.x.ai/build/features/skills-plugins-marketplaces),
  [sessions](https://docs.x.ai/build/features/sessions),
  [llms.txt](https://docs.x.ai/llms.txt),
  [x.ai/changelog/build](https://x.ai/changelog/build)
  (Latest still **v1.0.70**, 2026-10-23),
  npm `@xai-official/grok` dist-tags, GitHub
  [xai-org/grok-build](https://github.com/xai-org/grok-build).
* **(source)** — sibling checkout `~/gitrepos/grok-build`, origin
  `maccavelli/grok-build`, upstream `xai-org/grok-build`. HEAD
  `2bdd1d6a6369` (2026-09-29) “Synced from monorepo”, `SOURCE_REV`
  `559751fdcec02d413e4c57c8832ab275e4f44980`. In-tree changelogs stop at
  **1.0.45**. The published repo is not archived; `pushed_at`
  2026-09-29T16:57:13Z; `has_issues=false`; CONTRIBUTING.md still refuses
  external PRs.
* **(record)** — this repository, cited by filename. Magic grok package
  `internal/provider/grok/`.

---

## Executive assessment

Grok Build has **stopped publishing sources that match the binary**. The
public tree froze at ~1.0.45 on 2026-09-29. The official changelog page and
RSS still stop at **1.0.70** (2026-10-23). The installed stable CLI is
**1.0.77** (2026-11-05), seven npm releases later, with **alpha at 1.0.79**.
Magic’s pin is **1.0.70**. For grok, the contract is the live binary plus ACP
initialize, not GitHub and not the changelog (0226 F2, still true, now worse).

The ACP stdio path Magic already uses **still works in shape**:
`protocolVersion: 1`, NDJSON JSON-RPC, `loadSession`, session list/resume/close
(no delete), image+embeddedContext prompts, HTTP+SSE MCP, auth capability
present, vendor `_meta` for fs_notify / hooks / toolOverrides. Official docs
and the live binary agree: **newline-delimited JSON-RPC**, not LSP
Content-Length. A Content-Length initialize is rejected with parse errors
(probed). Magic’s grok Spec already launches
`grok <globals> --no-auto-update agent --no-leader stdio` (MADR 0050 / 0081),
which matches current `--help` and the official ACP sample.

What 1.0.77 **does** change for Magic, relative to the committed 1.0.70
capture:

1. **Model catalog shrank and gained a Fast id.** Initialize `_meta.modelState`
   on this host lists four models: `grok-4.7`, **`grok-4.7-build-fast`** (new),
   `grok-4.6` (current/default), `grok-4.5`. The 1.0.70 capture listed fourteen,
   including older 4.x ids and Imagine. `grok models` prints the same four
   (probed). 4.7 / 4.7 Fast / 4.6 advertise **`xhigh`** reasoning and dual
   context windows **256000 and 500000**. 4.5 has no `xhigh`.
2. **Auth methods on a signed-in home include `cached_token` as default.**
   Initialize advertised `xai.api_key`, `cached_token`, `grok.com`, with
   `defaultAuthMethodId=cached_token`. The 1.0.70 keyed capture advertised
   only `xai.api_key` and `grok.com`. Magic already allow-lists
   `cached_token` then `xai.api_key` and never pins `grok.com` (0085, Spec
   `SafeAuthMethodIDs` / `AuthPreference`). 0234’s cold env-key probe was
   **not** repeated on 1.0.77.
3. **Initialize `_meta` grew flags Magic does not harvest:** `grokShell: true`,
   `x.ai/mcp/sdk: true`, `x.ai/pluginDirs: true`, `feedbackTraceOffer: false`,
   plus the 1.0.70 keys `cancelRewind`, `sessionRecap`, `voiceMode`,
   `mcpApps`. `initializeMeta` still reads only version, model catalog, and
   default auth method.
4. **Advertised ACP slash commands are unchanged** at seven names: `compact`,
   `always-approve`, `context`, `session-info`, `deep-research`, `workflow`,
   `goal`. Magic already maps the useful ones as `KindNative`.
5. **The grok command table is stale against grok Ops.** `commandTable` still
   marks `undo` / `usage` / `status` as `KindNone` from the 1.0.5 TUI-vs-ACP
   probe, while `Spec.Ops` implements `UndoLast`, `RuntimeUsage`, and
   `RuntimeStatus` (0138 / 0208) and `sessioncaps.go` therefore advertises
   `CapUndo` / `CapUsage` / `CapStatus`. Slash `/undo` `/usage` `/status` stay
   hidden; the capability bits do not.

Do **not** treat `grok agent serve` / `headless` / `leader` or Cursor
`grok worker` as a second Magic transport. Those are Grok’s own TUI/relay/
Cursor surfaces. Magic’s grok path remains ACP stdio, the same way OpenCode v2
kept ACP for sessions (0214) rather than replacing it with every HTTP route.

A re-pin is warranted, and it is a 0226-style procedure: isolate a grok home,
run `make live-grok`, commit a `testdata/wire/1.0.77/` capture, then move
`KnownGoodVersion`. This report is not that procedure.

---

## 1. Publishing freeze

| Channel | What it currently says | Date |
| --- | --- | --- |
| GitHub `xai-org/grok-build` | Public, Apache-2.0, not archived, 51 commits, last push `2bdd1d6a` “Synced from monorepo” | 2026-09-29T16:57:13Z (docs) |
| In-tree `SOURCE_REV` | `559751fdcec02d413e4c57c8832ab275e4f44980` | same dump |
| In-tree shell changelogs | last files `1.0.45.md` / `1.0.45.json` | 2026-09-29 (source) |
| Official changelog + RSS | **Latest v1.0.70** | 2026-10-23 (docs) |
| npm `@xai-official/grok` `latest` | **1.0.77** | 2026-11-05T04:52:31Z (probed) |
| npm `alpha` | **1.0.79** | 2026-11-08T00:48:03Z (probed) |
| Installed binary / `grok update --check` | 1.0.77 stable, updateAvailable=false, installer=npm | probed |
| Magic `KnownGoodVersion` | `"1.0.70"` | `internal/provider/grok/version.go` (record) |

npm versions exist for 1.0.71 through 1.0.79. The changelog page was not
updated for any of them. Third-party mirrors (xstack, Model Drop) also lag or
summarise TUI notes; they are not a contract.

**Implication for Magic.** 0226 F2 already said grok source cannot show the
change after 1.0.45. That gap is now **32 published CLI versions**. Type
transcriptions in `ops.go` that cite
`~/gitrepos/grok-build/crates/codegen/xai-grok-shell/...` describe 1.0.45
shapes. Live 1.0.70 tests still decoded rewind (snake_case) and billing
(mixed case). They have not been shown to still decode on 1.0.77.

Treat GitHub and the changelog as **lagging**. Evidence for a pin is a live
capture. Do not bump `KnownGoodVersion` from this report.

---

## 2. Installed binary: transports and CLI surface

### 2.1 Process model (probed `--help`)

Top-level: interactive TUI (`grok [PROMPT]`), headless (`-p` / `--prompt-file`
/ `--prompt-json`), plus subcommands.

**ACP / agent transports** (`grok agent`):

| Subcommand | Role | Magic today |
| --- | --- | --- |
| `stdio` | ACP JSON-RPC on stdin/stdout | **this is the grok Spec** |
| `serve` | WebSocket server, default bind `127.0.0.1:2419`, optional `--secret` / `GROK_AGENT_SECRET`, optional `--remote` proxy | unused |
| `headless` | Grok WebSocket relay (`--grok-ws-origin` / `--grok-ws-url`) | unused |
| `leader` | shared leader on `~/.grok/leader.sock`; `--relay-on-demand`, `--cursor-worker*` | unused; Spec passes `--no-leader` |

Official ACP sample (docs): `grok agent stdio`, NDJSON `initialize` →
`authenticate` (`xai.api_key` if `XAI_API_KEY` else `cached_token`) with
`_meta.headless: true` → `session/new` → `session/prompt`. Assistant text
arrives as `session/update` `agent_message_chunk`. Magic already authenticates
from `SafeAuthMethodIDs` / `AuthPreference` and never uses `grok.com` on a
headless host (0085).

`--no-auto-update` remains a **global** flag. Official headless/ACP guidance
says to pass it in scripts; Magic already does, unconditionally, before
`agent` (0081 P1.3). `grok agent --help` still rejects most globals; 0050
placement still holds.

**Cursor private worker** is a separate top-level `grok worker`
(`start`/`stop`/`status`/`controller`) plus `grok agent leader --cursor-worker`.
That is Cursor Cloud Agents on this machine. It is not an ACP client Magic
should speak.

### 2.2 Headless output (docs + `--help`)

`--output-format`: `plain`, `json`, `streaming-json`, and **new relative to
Magic’s 1.0.5-era notes**, `streaming-messages-json`. Also
`--include-partial-messages`, `--json-schema`, `--prompt-json`, `--verbatim`,
`--max-turns`. These are the `-p` scripting path, not ACP. Magic does not
need them for the grok provider.

### 2.3 Auth CLI (probed)

`grok login --oauth` (loopback browser) and `--device-auth` / `--device-code`
(headless). `grok logout` clears cached credentials. Phone 0074 already writes
`~/.grok/config.toml` keys and has a grok device-auth path. Browser `grok.com`
ACP method remains unsafe for the daemon.

### 2.4 MCP, plugins, skills (probed + docs)

`grok mcp`: `list` / `add` / `remove` / `enable` / `disable` / `doctor`.
`add` takes `--transport stdio|http|sse`, `--scope user|project`, `--env`,
`--header`. Official docs: `${VAR}` expansion, OAuth tokens in
`~/.grok/mcp_credentials.json`, merge of `~/.claude.json`, `.cursor/mcp.json`,
`.mcp.json`. `list` and `doctor` accept `--json`.

`grok plugin`: `list` / `install` / `uninstall` / `update` / `enable` /
`disable` / `details` / `validate` / `tag` / `marketplace`
(`list`/`add`/`remove`/`update`).

Skills: `./.grok/skills/`, `~/.grok/skills/`, plugin `skills/`, `[skills] paths`,
and `~/.agents/skills/` / `~/.agents/commands/` (docs). User-invocable skills
become slash commands.

Magic already has agent-neutral plugin and skill MADRs
([0228](../decisions/0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md),
[0229](../decisions/0229-MADR-make-skill-discovery-freshness-and-refresh-agent-neutral.md)).
0081 left `grok plugin marketplace` as an operator utility. 0106 deferred ACP
`x.ai/pluginDirs` injection. 1.0.77 initialize still advertises
`x.ai/pluginDirs: true` and `x.ai/mcp/sdk: true` (probed). Grok Spec still
does not set `Ops.RefreshSkills` (0229). Those records remain the place to
adopt, not this report.

### 2.5 Sessions, worktrees, memory, usage (probed + docs)

| CLI | Notes | Magic |
| --- | --- | --- |
| `grok sessions list\|search\|delete` | delete is CLI-only; ACP `sessionCapabilities` still has no `delete` | daemon owns session identity (0095); grok `sessions/changed` stays declined |
| `grok export` | markdown transcript | unused |
| `grok inspect --json` | discovered config: rules, skills, plugins, hooks, MCP | unused; useful diagnostics input for 0228/0229 |
| `grok memory clear --workspace\|--global\|--all` | cross-session memory | unused |
| `grok usage [SESSION_ID] [TURN] --json` | plan limit or persisted session cost | ACP `_x.ai/session/usage` and `_x.ai/billing` already wired as Ops |
| `grok worktree` | create/list/show/rm/gc/detach/salvage/clean-artifacts/redirect/db; Grove lazy clone | unused; TUI `--worktree` |
| `grok clone` | Grove NFS/FUSE lazy clone | unused |
| `grok wrap` | local PTY + OSC 52 clipboard | unused |
| `grok doctor [--json] [fix]` | terminal/clipboard/color | unused |
| `grok dashboard` | Agent Dashboard | unused |
| `grok setup` | managed configuration | unused |
| `grok trace` | export/upload traces | unused |
| `grok update --check --json --alpha --stable --version` | installer-aware | Magic does not update agent CLIs |

`/share` was **removed** in grok 1.0.51 (changelog). Magic never wrapped it.

### 2.6 Spawn flags Magic already maps vs new ones

Already in `defaultArgs` (record): `-m`, `--reasoning-effort`,
`--always-approve`, `--permission-mode`, `--sandbox`, `--no-subagents`,
`--disable-web-search`, `--no-auto-update`, `agent`, `--no-leader`, `stdio`.

Present on 1.0.77 and **not** in grok `defaultArgs`:

* `--allow` / `--deny` (permission rules)
* `--tools` / `--disallowed-tools`
* `--no-plan`
* `--restore-code` (Magic already sends `x.ai/restore_code: false` on
  session `_meta`, 0138 Phase 11)
* `--fork-session`, `-w` / `--worktree`, `--worktree-ref`
* `--add-dir`, `--agent` / `--agents`, `--rules`, `--system-prompt-override`
* `--max-turns`, `--minimal`, `--oauth`, `--leader-socket`

Adopt a spawn flag only after an ACP discrimination pair (0106’s rule).
`--permission-mode plan` is documented as actually starting plan mode
(changelog 1.0.65); Magic still synthesizes plan via ACP `session/set_mode`
and `auto` via permission intercept (0049).

---

## 3. ACP protocol (probed initialize on 1.0.77)

### 3.1 Framing

| Send | Result |
| --- | --- |
| LSP `Content-Length` + JSON body | grok stderr: `failed to parse incoming message: expected value at line 1 column 1. Raw: Content-Length: 224`. No initialize result. |
| NDJSON (`{...}\n`) | initialize result, 5251 bytes, `agentVersion: "1.0.77"`. |

Official JS sample writes `JSON.stringify(...) + "\n"`. Magic 0002 already
named grok ACP as NDJSON JSON-RPC 2.0 on stdio. acpagent rewind tests comment
the same. **Do not switch Magic’s grok transport to Content-Length.**

### 3.2 Standard ACP fields vs 1.0.70 capture

| Field | 1.0.70 capture | 1.0.77 probe |
| --- | --- | --- |
| `protocolVersion` | 1 | 1 |
| `agentInfo` | absent | absent (`null`) |
| `loadSession` | true | true |
| `promptCapabilities` | image true, audio false, embeddedContext true | same |
| `mcpCapabilities` | http true, sse true | same |
| `sessionCapabilities` | list, resume, close; **no delete** | same |
| `auth` | `{}` | `{}` |
| `agentCapabilities._meta` | `x.ai/fs_notify`, `x.ai/hooks`, `x.ai/capabilities.toolOverrides` | same keys and same override booleans |

Hook `_meta` is unchanged: blocking events `pre_tool_use`, `stop`,
`subagent_stop`; decisions `deny`, `block`; stopSignals `continue`,
`stopReason`, `additionalContext`.

Tool overrides still: `x_keyword_search` true, `x_semantic_search` true,
`x_user_search` false, `x_thread_fetch` false. Magic does not surface those
overrides.

Version still lives in `_meta.agentVersion`, not standard `agentInfo`
(0137 ninth amendment; `version.go` comment). 1.0.77 did not add `agentInfo`.

### 3.3 Auth methods

1.0.77 on the operator home (probed):

| id | name | description |
| --- | --- | --- |
| `xai.api_key` | `xai.api_key` | `XAI_API_KEY` or `api_key`/`env_key` in config.toml |
| `cached_token` | `cached_token` | Cached token from `~/.grok/auth.json` |
| `grok.com` | Grok | Sign in with Grok |

`defaultAuthMethodId`: `cached_token`.

1.0.70 committed capture: `xai.api_key`, `grok.com` only. 0234 F5 showed a
cold env-key 1.0.70 also omitted `cached_token`. This 1.0.77 run used the
operator home, so the extra method is **credential-state**, until a cold
scratch home is probed again.

Magic Spec (record): `SafeAuthMethodIDs = cached_token, xai.api_key`;
`AuthPreference` the same order; `KeyAuthMethodID = xai.api_key`;
`HasHeadlessCredential = grokHasAPIKey` (config.toml / `XAI_API_KEY`, not
auth.json). That already matches the official sample’s preference. Keep
`grok.com` off the allow-list.

### 3.4 Model catalog (probed)

`grok models` and initialize `_meta.modelState` agree:

* current / CLI default: **`grok-4.6`**
* available: `grok-4.7` (“SpaceXAI's latest frontier model”),
  `grok-4.7-build-fast` (“Fast variant. 2x the price.”), `grok-4.6`, `grok-4.5`

| id | context | reasoning ladder | notes |
| --- | --- | --- | --- |
| grok-4.7 | 256k listed; `contextWindows` 256000 and 500000 | xhigh, high (default), medium, low | agentType `grok-build-plan` |
| grok-4.7-build-fast | same | same | docs: Fast is CLI/Cursor only, not the public API |
| grok-4.6 | same | same | current on this host |
| grok-4.5 | same windows | **no xhigh**; high default, medium, low | |

0235 takes the floor catalog from go-llmprovider-sdk’s grok list. A re-pin
PLAN must check whether the SDK already names `grok-4.7-build-fast` and
`xhigh`, and whether `TestTheFloorIsInTheKnownGoodCapture` still holds when
the capture lists four ids instead of fourteen. The 1.0.70 capture’s extra
ids were a signed-in catalog of that day; 0235 D1 says a live catalog
replaces the floor outright.

`initializeMeta` already builds a live picker catalog from
`availableModels`, including thinking levels when
`supportsReasoningEffort` is true. Dual `contextWindows` are **not** read.

### 3.5 Advertised commands (initialize `_meta.availableCommands`)

Same seven names as 1.0.70:

| name | input hint | Magic `commandTable` |
| --- | --- | --- |
| compact | — | KindNative `compact` (Ops.Compact also exists) |
| always-approve | `on\|off` | forwarded via caveat; TUI-only silence still possible |
| context | — | KindNative `session-info` |
| session-info | — | used as native for context |
| deep-research | `<query>` | KindNative |
| workflow | name / runs / pause\|resume\|stop\|save | KindNative |
| goal | objective / status / pause / resume / clear | KindNative |

Official TUI command list is much larger (`/rewind`, `/fork`, `/rename`,
`/loop`, `/imagine`, `/tasks`, `/create-workflow`, `/usage`, `/plugins`,
…). Magic’s table already says grok’s advertised list is the TUI catalog
and several entries are silent over ACP. 1.0.77 did not add ACP-advertised
commands.

### 3.6 New / still-unread initialize `_meta` keys

Harvested today: `agentVersion`, `modelState`, `defaultAuthMethodId`.

Present on 1.0.77 and ignored by `initializeMeta`:

| key | 1.0.77 value | prior |
| --- | --- | --- |
| `grokShell` | true | not in Magic harvest |
| `x.ai/mcp/sdk` | true | new relative to 1.0.70 capture summary |
| `x.ai/pluginDirs` | true | advertised since 1.0.5; 0106 deferred injection |
| `feedbackTraceOffer` | false | new |
| `cancelRewind` | true | 1.0.70 already true; Ops.UndoLast uses rewind |
| `sessionRecap` | true | 1.0.70; unused |
| `voiceMode` | true | 1.0.70; unused |
| `mcpApps` | false | 1.0.70 |
| `mcpServers` | `[]` | listed, empty in the probe cwd |
| `availableCommands` | seven rows | harvested from the live session, not initialize |
| `hostname` / `agentId` / `agentInstanceId` / `currentWorkingDirectory` | present | redact; do not persist host identity |

---

## 4. Magic grok machinery vs 1.0.77

### 4.1 What already matches and can stay

* Launch argv: globals then `--no-auto-update agent --no-leader stdio`.
* NDJSON ACP stdio. Do not add Content-Length.
* Safe auth: `cached_token`, `xai.api_key`; never `grok.com`.
* Phone key write to config.toml; device auth separate (0074).
* Session `_meta`: `modelId`, `reasoningEffort`, `x.ai/restore_code: false`.
* Extension **requests**: `_x.ai/exit_plan_mode`, `_x.ai/ask_user_question`.
* Extension **notifications** handled: models/update, MCP status/init/progress,
  session_notification (subagents, retry_state, turn_completed usage,
  auto_compact), session/update replay costs.
* Extension **notifications declined** (0137): announcements/update,
  settings/update, sessions/changed, queue/changed, session/prompt_complete.
  1.0.77 initialize did not advertise a new notification method. A live turn
  capture is still required to know whether grok added any.
* Ops: Fork `_x.ai/session/fork`; Compact `_x.ai/compact_conversation`;
  Rename `_x.ai/session/rename`; UndoLast rewind points/execute;
  RuntimeUsage `_x.ai/session/usage`; RuntimeStatus `_x.ai/billing`;
  ConfirmLimit via billing. Live tests exist for 1.0.70, not 1.0.77.
* Static modes `default` / `plan`; `SynthesizeAutoMode`.
* Floor models from go-llmprovider-sdk (0235), with live catalog override.

### 4.2 What needs updating (observation, not a decision)

These are the items a 0226-style grok re-pin PLAN would have to name.

1. **`KnownGoodVersion` and capture.** Still `"1.0.70"`. Installed is 1.0.77.
   Drift warns and never refuses (`version.go`). Re-pin only after
   `make live-grok` against 1.0.77 with an isolated grok home, and a committed
   `testdata/wire/1.0.77/`. Keep 1.0.70 (and 1.0.13 full-turn) as historical
   named captures, the way 1.0.13 was kept at 0226.
2. **`commandTable` vs Ops.** Table still says grok cannot undo, usage, or
   status over ACP. Ops and `Cap*` say it can. Canonical specs default `undo`
   to `KindOp`; grok’s explicit `KindNone` wins and hides the slash command.
   A PLAN should remap:
   * `undo` → `KindOp` `OpUndo` (rewind last point, `cancelRewind: true`)
   * `usage` → `KindOp` `OpUsage` (`_x.ai/session/usage`)
   * `status` → `KindOp` `OpStatus` (`_x.ai/billing`; API-key installs already
     get a text “unavailable” from `runtimeStatus`)
   Leave `redo` as `KindNone` (no un-rewind). Leave `diff` as `KindNone`
   unless a live probe finds a vendor method.
3. **Live model catalog.** Four ids including `grok-4.7-build-fast`; `xhigh`;
   dual context windows. 0235 floor + saved catalog must be re-checked against
   a 1.0.77 capture. Do not hand-edit `staticModels`; it is SDK-derived.
4. **0234 cold env-key.** Repeat the isolated-home initialize on 1.0.77. If
   `cached_token` is still omitted without auth.json, 0234 D2/D3 still hold.
5. **`x.ai/pluginDirs` and `x.ai/mcp/sdk`.** Still advertised. 0106 deferred
   the former. 0228/0229 are the agent-neutral place to consume plugin/MCP/skill
   inventory (`grok inspect --json` is a new machine-readable side channel).
6. **Rewind / billing JSON shape.** `ops.go` comments still cite 1.0.45 Rust
   types. Re-run `live_rewind_test` and `live_runtime_test` on 1.0.77 before
   trusting snake_case vs camelCase.
7. **Changelog-only behaviour that can leak onto ACP.** 1.0.51 removed
   `/share`. 1.0.65 made `--permission-mode plan` actually start plan mode.
   1.0.67 worktree approval inheritance. 1.0.77 TUI notes (xstack): kitty
   keyboard, quote copy. Most of that is TUI. Discriminate before adopting
   spawn flags.

### 4.3 New features worth adopting (later MADR, after a pin)

Highest leverage, still on ACP stdio:

* **Wire command table to existing Ops** (undo/usage/status). No new RPC.
* **`grok-4.7-build-fast` and `xhigh` in the live picker.** Mostly falls out
  of initialize harvest + 0235 once the capture moves.
* **Context window choice.** Models advertise `contextWindows: [256000, 500000]`.
  Magic does not send a window. Worth a discrimination pair if grok honors it
  on `session/new` `_meta`.
* **Plugin/skill/MCP diagnostics via `grok inspect --json` and initialize
  `x.ai/pluginDirs`.** Feeds 0228/0229 without a second transport.
* **Native `/loop` and `/create-workflow`.** Already `KindNative` for `loop`
  and `workflow`. Confirm they produce ACP `session/update` on 1.0.77 rather
  than TUI silence (0208 D10 caveat still applies to `always-approve`).
* **`/rewind` as a targeted undo**, not only “last turn”. Ops.UndoLast always
  takes the highest `prompt_index`. A later record could expose a point list
  if the phone grows a rewind UI. The vendor methods already exist.

Do **not** adopt as Magic grok transports:

* `grok agent serve` / `headless` / `leader` WebSocket
* Cursor `grok worker`
* Grove `clone` / worktree NFS
* `grok wrap`
* Imagine / voice TUI commands (`voiceMode: true` is a TUI flag)

Those expand Grok the product; they do not change the ACP session Magic
already multiplexes.

### 4.4 Optimizations

* **Stop treating GitHub grok-build as a type oracle.** After 1.0.45 the
  comments in `ops.go` that quote Rust structs are historical. Pin shapes
  from live captures (already the 0226 rule). Optionally copy the 1.0.77
  initialize result into `testdata/wire/1.0.77/` even before a full live
  suite, as a harvest fixture only.
* **Isolate ACP probes.** This report’s initialize used the operator grok
  home. A re-pin must set a scratch `GROK_HOME` / config dir so model lists
  and auth methods are reproducible (0234).
* **Command caveat.** 0208 D10 marks TUI-only advertised commands that stay
  silent. Re-measure `always-approve` and `context` on 1.0.77; compact is
  already known-native.
* **Do not parse changelog RSS for pin bumps.** It is 7+ releases behind.
  `grok update --check --json` and npm `dist-tags.latest` are the version
  oracles.
* **Alpha 1.0.79 exists.** Do not pin alpha. `grok update --check` on this
  host stays on stable 1.0.77.

---

## 5. Official docs vs binary vs Magic

| Claim | Docs | Binary 1.0.77 | Magic |
| --- | --- | --- | --- |
| ACP command | `grok agent stdio` | same; `--no-leader` available | adds `--no-auto-update` and `--no-leader` |
| ACP framing | NDJSON sample | NDJSON only (probed) | NDJSON (0002, acpagent tests) |
| Auth for scripts | `XAI_API_KEY` or `grok login`; sample prefers key then cached_token | three methods when signed in | key + cached_token allow-list |
| `--no-auto-update` | required for ACP/headless scripts | global flag | unconditional |
| Changelog latest | 1.0.70 | 1.0.77 | pin 1.0.70 |
| Source | README points at GitHub | binary 32 versions later | 0226 F2 already recorded the freeze |
| `grok models --json` | CLI reference lists `grok models` without `--json` | `--json` is **unexpected argument** (probed earlier) | n/a |
| `grok inspect --json` | documented | present | unused |
| Session delete | `grok sessions delete` | present | ACP has no delete capability |
| `/share` | absent from current modes page | removed 1.0.51 | never wrapped |

`docs.x.ai/.../*.md` URLs 404 for several CLI pages; the same paths without
`.md` serve markdown via `Accept`. `llms-full.txt` 404s. `llms.txt` is the
reliable index.

---

## 6. Recommended later work (this report does not start it)

A grok 1.0.77 re-pin pair, same shape as 0226:

1. Isolated grok home. Cold initialize (0234) plus signed-in initialize.
2. `make live-grok` against 1.0.77. Fail-first on a scratch copy if any
   live test is extended.
3. Commit `testdata/wire/1.0.77/` (initialize plus the same frame kinds
   1.0.70 kept). Point `KnownGoodVersion` at `"1.0.77"`.
4. Remap `commandTable` undo/usage/status to the existing Ops.
5. Confirm 0235 floor ⊆ 1.0.77 catalog, including whether Fast and `xhigh`
   need an SDK catalog bump (that bump is go-llmprovider-sdk, not this tree).
6. Leave pluginDirs / inspect-json / contextWindows to 0228/0229 or a new
   pair once the pin exists.

Out of scope for that pair: gobble Register (0186 D9), OpenCode/Kilo/Codex
pins, `grok agent serve`, Cursor worker, Grove.

---

## 7. Probe limits

* No `session/new` or `session/prompt` on 1.0.77, so vendor notifications
  during a turn, config options on `session/new`, and TUI-only command
  silence were not re-measured.
* Initialize used the operator grok home. Auth method list and model catalog
  are that home’s.
* Content-Length vs NDJSON was initialize-only.
* Alpha 1.0.79 was not installed.
* grok-build source was not diffed against 1.0.77; it cannot be.

---

## Sources

* Installed `grok` 1.0.77 `(50ca579fa996)` [stable]; `--help` of agent,
  stdio, serve, headless, leader, login, mcp, plugin, worker, sessions,
  memory, usage, models, inspect, worktree, wrap, doctor, update, clone.
* ACP initialize NDJSON result saved during the probe (not committed;
  hostname redacted in this report).
* `internal/provider/grok/{grok.go,version.go,commandtable.go,ops.go,initialize.go,auth.go,notifications.go,requests.go,fallback.go}`.
* `internal/provider/grok/testdata/wire/1.0.70/{frames.jsonl,meta.json}`.
* Official docs listed in the preamble; GitHub API `pushed_at`; npm
  `dist-tags` and `time`.
* Records 0002, 0050, 0074, 0081, 0085, 0106, 0137, 0138, 0208, 0226, 0228,
  0229, 0234, 0235.
