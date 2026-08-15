---
status: accepted
date: 2026-08-15
decision-makers: Project Owner (scope and acceptance); Implementer (probe)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Pin Kilo known-good to 7.4.22 and close the session-loop gaps that release added

## Context and Problem Statement

[0075](./0075-MADR-kilo-cli-provider.md) shipped the Kilo provider against
**kilo 7.4.20**. [0087](./0087-MADR-kilo-session-chrome-and-permission-decode.md)
fixed three live regressions on **7.4.22** but its D6 left
`KnownGoodVersion = "7.4.20"`. That pin is the contract for “every wire
shape in `internal/provider/kilo` was live-probed.” Leaving it on a
release the host no longer runs is not acceptable: the engine on PATH
is 7.4.22 (`/home/mac/.local/bin/kilo`), npm `@kilocode/cli@7.4.22`
published 2026-08-13, and JetBrains already pins Core 7.4.22.

This record asks: **what is the full 7.4.22 surface (CLI, HTTP, SSE,
docs, release notes), what does mcremote already cover, and what must
we take, defer, or reject to make 7.4.22 the known-good pin?**

Architectural scope: `internal/provider/kilo` dialect + its
`httpagent` host, command table, live catalogs, and `live_kilo` tests.
Not kilo Cloud, not Agent Manager IDE chrome, not `kilo run` as a
control plane (0075 unchanged).

## Decision Drivers

* The known-good pin must name a release we have driven, not a
  historical spike.
* Phone session loop remains the product: prompt, stream, tools,
  permissions, modes, slash commands, resume.
* Wire-shape claims need live or fixture evidence (AGENTS.md).
* Do not dual-render 7.4.22’s new `session.next.*` stream until a live
  turn proves `message.part.*` is gone (0087 D4 still holds).
* Keep the un-gated loopback engine (0075 D5 / PD1). New endpoints that
  require Basic when `KILO_SERVER_PASSWORD` is unset are not free.
* Official docs and GitHub release notes are the product intent; the
  live `/doc` + `GET /agent` + `GET /command` are the wire truth.

## Considered Options

* Option 1: Pin 7.4.22 and take the session-loop deltas (chosen)
* Option 2: Stay on the 7.4.20 pin and treat 7.4.22 as “best effort”
* Option 3: Dual-track 7.4.20 and 7.4.22 dialects / version gates

## Decision Outcome

Chosen option: **"Option 1: Pin 7.4.22 and take the session-loop
deltas"**, because the installed engine, npm, and JetBrains pin are
already there; the 7.4.20 spike is still valid evidence for the
*shared* HTTP+SSE shape, but it is no longer the release we ship
against. 0087 D1–D5 stay; 0087 D6 is superseded here.

* Implementation Plan:
  [0088-PLAN-kilo-7.4.22-surface-parity.md](./0088-PLAN-kilo-7.4.22-surface-parity.md)

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | Set `KnownGoodVersion = "7.4.22"`. Refresh the `OnHealthy` comment and the 0075 “known-good CLI” line to point here. Keep the 7.4.20 spike dir as historical evidence; do not delete it. |
| **D2** | 0087 D1–D5 (chrome filter, silent `session.diff`, `properties`/`data` decode, ignore `session.next.synthetic`, per-permission `once`) are **in** the 7.4.22 pin. They are not optional follow-ups. |
| **D3** | Primary turn stream stays `message.part.delta` / `message.part.updated` + `session.status` / `session.idle` / `session.turn.*`. Map `session.next.text.delta` / `session.next.tool.*` / `session.next.shell.*` only after a live successful turn shows those events **without** the part stream. Until then they are ignore-or-dedup, not a second transcript. |
| **D4** | Live `GET /command` is the slash-command source of truth. On 7.4.22 that list includes **`review`**, **`resume-claude`**, **`resume-codex`**, plus `init` and any project/skill-backed names. Flip `commandtable.go`’s `review` from `KindNone`/`ReasonNoReview` to `KindOp` (or `KindAgent` via `POST …/command`). Advertise the resume-import commands the same way. |
| **D5** | Native tools on `code` in 7.4.22 include **`grep`** (`kilo debug agent code` → `tools.grep: true`). `bash *` is still **ask** in the merged permission ruleset. Auto mode must keep answering bash asks (0087 D3 + 0044). Do not assume grep is bash-only. |
| **D6** | Skills (`GET /skill`, `skill` tool, official [Skills](https://kilo.ai/docs/customize/skills) docs) are **engine-side**. mcremote does not need a skills picker for the pin. The agent already loads them. Optional later: surface the skill list as read-only session metadata. |
| **D7** | Worktrees (`kilo worktree`, `GET/POST/DELETE /experimental/worktree`, `worktree.*` events) and sandbox (`POST /session/{id}/sandbox/toggle`, unavailable on this host — no Bubblewrap) are **out of the 7.4.22 pin**. Phone CWD is the session directory; we do not create worktrees or toggle sandbox. |
| **D8** | PTY / `POST /session/{id}/shell` / `interactive_terminal.*` stay unused. `code` exposes `interactive_terminal` as a tool; we do not host a phone PTY. Bash via permission.asked remains the remote command path. |
| **D9** | New v2 aliases (`POST /api/session/{id}/agent`, `/model`, `/interrupt`, `GET …/history`, revert stage/commit/clear) are optional fallbacks. Keep the v1 routes we already call (`prompt_async`, `abort`, `SetModel`’s existing path). Adopt v2 only if a v1 call 404s on 7.4.22 (none did in this probe). |
| **D10** | `POST /permission/allow-everything` stays unused for auto (0087 D5). Revisit only with an explicit 0075 D5 amendment (Basic `kilo:` on a passwordless engine). |
| **D11** | Out of scope, unchanged from 0075: `kilo run` as control plane, `kilo remote` / Cloud Agents, Agent Manager IDE, `/kilocode/agent-manager*`, notebook requests, speech-to-text, clickable file spans (7.4.22 #11219 — IDE). |

### Priority of work this pin requires

Must land with the pin (session-loop correctness):

1. D1 version constant + comment + 0075 erratum line.
2. D2 already in the tree (0087 implementation).
3. D4 `review` + `resume-claude` + `resume-codex` command-table / advertise.
4. D5 regression tests: chrome filter + durable permission decode (0087
   unit tests) + a `live_kilo` permission/tool turn when a non-paid
   model is available.
5. Fixture refresh: save 7.4.22 `/doc` path list + agent/command
   summaries next to the 7.4.20 spike (or a thin
   `docs/kilo-spike-7.4.22/` README pointing at the probe).

Should land soon after (uptake, not blockers):

6. Live-turn histogram that lists every `session.next.*` type actually
   emitted on a successful tool turn; reopen D3 if `message.part.*`
   disappears.
7. Optional skill-list event (D6) if the phone needs a “skills loaded”
   affordance.
8. `kindForTool("grep")` is already `search`; confirm tool cards for
   native grep vs bash grep.

Explicitly not in this pin: D7, D8, D10, D11.

## Consequences

* Good, because doctor/logs stop warning “engine version differs from
  known-good” on every boot of the current binary.
* Good, because `/review` and the resume-import commands stop being
  advertised as “Kilo cannot do this” while the engine lists them.
* Good, because the 7.4.22 permission and chrome bugs stay part of the
  pin instead of living in a “later pass” that never gets a version
  number.
* Bad, because a 7.4.20-only host will log a skew warning the other
  way. That is the correct signal.
* Bad, because we still have no completed live tool turn on this host
  (`kilo-auto/free` → `PAID_MODEL_AUTH_REQUIRED`). D3’s “part stream
  still exists” claim is from the failed-auth turn plus the 7.4.20
  spike, not a 7.4.22 PONG+grep success.
* Neutral, because worktrees, PTY, Cloud, and Agent Manager remain
  documented gaps rather than silent pretences of support.

## Pros and Cons of the Options

### Option 1: Pin 7.4.22 and take the session-loop deltas (Chosen)

* Good, because the pin matches npm, this host, and JetBrains Core.
* Good, because the required code delta is small (version + command
  table + tests); 0087 already did the decode/filter work.
* Bad, because a live successful turn is still unpaid on this host, so
  D3 is provisionally “part stream first.”
* Neutral, because 7.4.20 fixtures remain as regression corpus.

### Option 2: Stay on the 7.4.20 pin

* Good, because no comment/constant churn.
* Bad, because every 7.4.22 boot logs a false “drift” warning and
  implies the dialect is unverified.
* Bad, because `/review` already exists on the wire and the table lies.

### Option 3: Dual-track dialects / minimum-version gate

* Good, because a 7.4.20 leftover install would keep working without
  skew noise.
* Bad, because we have one dialect and one event loop; a gate would
  split tests and never pay for itself at this fork distance (13 added
  paths, zero removed).
* Neutral, because a real minimum-version gate is still reserved for
  `session_tree` (0075 PD2), not this pin.

## Confirmation

* `kilo --version` and `GET /global/health` both report `7.4.22`.
* `KnownGoodVersion == "7.4.22"`; `OnHealthy` logs info, not warn, on
  this host.
* `CommandTable()["review"]` is not `KindNone`.
* `go test -race ./internal/provider/kilo/` includes 0087’s chrome and
  durable-envelope tests.
* `go test -tags live_kilo` permission + prompt tests pass on a host
  with a usable model (acceptance; not this machine’s free router).
* Support matrix in §More Information is re-probed after the next
  CLI minor (7.5.x) before the pin moves again.

## More Information

### Probe (this host, 2026-08-14)

| Item | Result |
| --- | --- |
| Binary | `/home/mac/.local/bin/kilo` → 7.4.22 |
| Health | `GET /global/health` → `{"healthy":true,"version":"7.4.22"}` |
| OpenAPI | `GET /doc` OpenAPI 3.1.0 title `kilo`, **256** paths (**+13** vs 7.4.20 spike 243), **681** schemas (spike 453) |
| Paths removed | **0** |
| SSE event type enums | **119** |
| Agents | 10: primary `code` `ask` `debug` `plan` `orchestrator`; hidden `compaction` `summary` `title`; subagents `explore` `general` |
| Live commands | `init`, **`review`**, **`resume-claude`**, **`resume-codex`**, plus project/skill-backed `kilo-config`, `writing-madr-and-plans` |
| Skills | `GET /skill` returns builtin `kilo-config` + host skills (`.agents/skills`, …) |
| Default model | `kilo` → `kilo-auto/free` (unauthenticated); paid-model 401 on prompt |
| Sandbox | `{"available":false,"reason":"No usable Bubblewrap executable is available"}` |
| `allow-everything` | 401 without Basic; 200 with `-u kilo:` |
| Permission reply | 404 without Basic (not gated) |
| Experimental | `GET /experimental/capabilities` → `{"backgroundSubagents":false}` |

Added HTTP paths vs [docs/kilo-spike-7.4.20/openapi-paths.txt](./kilo-spike-7.4.20/openapi-paths.txt):

```
GET  /api/session/active
POST /api/session/{sessionID}/agent
GET  /api/session/{sessionID}/event
GET  /api/session/{sessionID}/history
POST /api/session/{sessionID}/interrupt
GET  /api/session/{sessionID}/message/{messageID}
POST /api/session/{sessionID}/model
GET  /api/session/{sessionID}/permission/{requestID}
POST /api/session/{sessionID}/revert/clear
POST /api/session/{sessionID}/revert/commit
POST /api/session/{sessionID}/revert/stage
GET  /kilocode/command/files
POST /kilocode/command/remove
```

`code` agent tools (`kilo debug agent code`): `bash`, `read`, `glob`,
**`grep`**, `edit`, `write`, `task`, `webfetch`, `todowrite`,
`websearch`, `skill`, `question`, `suggest`, `kilo_local_recall`,
`background_process`, `interactive_terminal`, `plan_exit`. Permission
rules still include `bash * ask` and `doom_loop`/`external_directory`/
memory `ask`.

### Official docs (fetched 2026-08-14)

| Source | What it adds for us |
| --- | --- |
| [CLI](https://kilo.ai/docs/code-with-ai/platforms/cli) | `/auto-approve` is TUI global config, not an agent. `/review` is a first-class slash command (uncommitted / branch / commit / PR). `/reload` rescan skills. `/resume-claude` `/resume-codex`. Permissions object syntax (`bash: { "grep *": "allow" }`). |
| [CLI reference](https://kilo.ai/docs/code-with-ai/platforms/cli-reference) | `kilo serve` still hostname/port/pure/mdns. `kilo run --auto` / `--variant`. New: `kilo worktree`, `kilo cloud`, `kilo remote`, `kilo roll-call`. `kilo console` deprecated. |
| [Skills](https://kilo.ai/docs/customize/skills) | Agent Skills spec; loaded at session start; `skill` tool; trusted-only `!`cmd`` in SKILL.md. |
| [GitHub v7.4.22](https://github.com/Kilo-Org/kilocode/releases/tag/v7.4.22) | Clickable file spans (IDE). Agent Manager PR comments (IDE). **Removed Morph WarpGrep.** Subagent permission inheritance fix (#12373) — writable subagents no longer inherit parent `readOnlyBash` as a ceiling. Upstream OpenCode 1.17.13 → 1.18.13 (MCP adapter, hide `execute` unless code mode, reasoning variants). |
| [GitHub v7.4.21](https://github.com/Kilo-Org/kilocode/releases/tag/v7.4.21) | `/review` nested modes (`staged`, `unpushed`, `quick`). Model catalog before session start. Worktree repo picker (Agent Manager). |

### Support matrix — mcremote vs kilo 7.4.22

Legend: **in** = dialect implements it · **pin** = required for this
pin · **later** = real surface, not this pin · **out** = rejected as
control plane or IDE-only.

| Surface | 7.4.22 engine | Dialect today | Pin |
| --- | --- | --- | --- |
| `kilo serve` loopback + `?directory=` | yes | in | in |
| Health + version | 7.4.22 | pin still 7.4.20 | **D1 bump** |
| `prompt_async` + abort + delete | yes | in | in |
| `message.part.delta/updated` text, reasoning, tools | yes | in | in (D3 primary) |
| Transient chrome (`synthetic`, dotted lifecycle) | yes | in (0087) | in |
| `session.status` / `idle` / `turn.*` / `error` | yes | in | in |
| `session.diff` SSE | snapshot + patch | silent (0087 D2) | in (stay silent) |
| `session.next.synthetic` | yes | ignored | in |
| `session.next.text/tool/shell/step` | OpenAPI yes | ignored | later (D3) |
| Durable `payload.data` events | OpenAPI yes | in (0087 D3) | in |
| Permission asked/v2 + reply `once/always/reject` | yes | in | in |
| `allow-everything` | 401 sans Basic | unused | out (D10) |
| Questions | yes | in | in |
| Modes `code/ask/debug/plan/orchestrator` + `auto` | yes | in | in |
| Native `grep` on `code` | `tools.grep: true` | kind=search | in (D5) |
| `bash *` ask | yes | auto `once` | in |
| `/init` | live command | in | in |
| `/review` | live command | **table says no** | **D4 flip** |
| `/resume-claude` `/resume-codex` | live commands | not advertised | **D4** |
| Fork / revert / unrevert / compact / model / undo / redo / diff | v1 yes; v2 aliases added | in via v1 | in (D9) |
| Skills list + `skill` tool | yes | engine-only | later (D6) |
| Worktrees | CLI + `/experimental/worktree` | no | later (D7) |
| Sandbox toggle | API; unavailable here | no | later (D7) |
| PTY / session shell / interactive_terminal | yes | no | later (D8) |
| Memory recall/save | ask on `code` | permission sheet / auto | in (generic perm) |
| Agent Manager / notebooks / STT | yes | no | out (D11) |
| Cloud / remote / `kilo run` | yes | no | out (D11) |
| ACP `kilo acp` | still present | unused (0075: serve is primary) | out |
| `--variant` reasoning | `kilo run --variant` | thinking = KindNone | later |

### Related

* [0075-MADR-kilo-cli-provider.md](./0075-MADR-kilo-cli-provider.md)
* [0076-MADR-kilo-debug-pass.md](./0076-MADR-kilo-debug-pass.md)
* [0087-MADR-kilo-session-chrome-and-permission-decode.md](./0087-MADR-kilo-session-chrome-and-permission-decode.md)
  (D6 superseded)
* [0044-MADR-auto-approve-modes.md](./0044-MADR-auto-approve-modes.md)
* [0023-MADR-canonical-slash-commands.md](./0023-MADR-canonical-slash-commands.md)
* Spike 7.4.20: [docs/kilo-spike-7.4.20/](./kilo-spike-7.4.20/)
