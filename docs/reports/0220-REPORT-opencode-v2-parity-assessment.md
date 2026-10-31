<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# OpenCode v2.2.3 parity assessment for the magic-cli-remote OpenCode provider

Probed 2026-10-30/31. This report observes; it decides nothing. The decision
that governs the OpenCode v2 work is
[0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](../decisions/0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md),
which is `status: proposed`. What this report adds to that record is fresh,
first-hand evidence against the **currently installed engine (v2.2.3)** — the
record's own probes were against v2.1.4 — plus a source-level inventory of both
codebases and a re-homing work breakdown a PLAN can be written from.

Evidence classes, marked as they appear:

* **(probed)** — measured live against the installed `opencode v2.2.3` binary
  (`/Users/<user>/.opencode/bin/opencode`, `opencode --version` →
  `opencode v2.2.3`), in a session scratchpad outside this repository, with
  `HOME`, every `XDG_*` directory, `OPENCODE_CONFIG_DIR`-equivalent paths and
  autoupdate/filewatcher pointing at scratch. No prompt was ever sent and no
  provider credentials exist in the scratch home, so nothing below spent quota
  or touched the owner's real OpenCode data.
* (source) — read in the sibling checkout
  `/Users/<user>/gitrepos/opencode`, whose HEAD is `237a847056` (2026-10-29)
  with all package versions reading `2.2.2`. Its committed
  `packages/protocol/openapi.json` matches the installed binary's live
  `/openapi.json` exactly — 134 paths / 162 operations on both (probed) — so
  this checkout is effectively the code that ships as 2.2.3. Opencode paths
  below are repo-relative to that checkout; a relative link cannot reach
  another repository.
* (record) — quoted from this repository's records, cited by full filename.

Host-identifying values (machine hostname from `/api/info`, process pids of the
owner's own running servers, real paths) are redacted throughout.

---

## Executive assessment

* The outage mechanism is fully verified end to end. The provider's version
  floor passes against v2.2.3 — `MinVersion = "1.18.0"`
  (`internal/provider/opencode/version.go:13`) and 2.2.3 ≥ 1.18.0 — so the
  engine boots, `/global/health` answers `200 text/html` (probed; the SPA web
  shell), every `200` counts as healthy (`internal/provider/httpagent/provider.go:624`),
  the empty version then skips `CheckMinVersion`
  (`internal/provider/opencode/http.go:717`), and the provider walks into an
  API that is not there (`POST /session?directory=` → 405, probed). SSE and
  health both accept a `text/html` 200 (`internal/provider/httpagent/provider.go:770-771`),
  so nothing in the boot path is loud.
* [0214]'s chosen architecture (option C — `acpagent` for sessions plus a
  lease-bound v2 HTTP control plane) remains the right shape, and every one of
  its structural claims reproduced on 2.2.3: `--pure` is still gone; the
  `--stdio` lease still prints exactly one JSON line and exits 0 at stdin EOF
  (probed); Basic auth is still `opencode:<password>` with the password sourced
  from the environment (probed); ACP `initialize` still answers with
  `protocolVersion: 1`, `agentInfo {version}`, the opencode
  `child-session-updates` `_meta`, and `opencode-login` as the only auth
  method (probed).
  To be precise about what is and is not history: none of it exists for
  opencode today. Opencode's only transport in this repository has ever been
  the 1.x HTTP/SSE dialect ([0019]'s process model); acpagent's one production
  user is grok (`internal/daemon/daemon.go:87`), and kilo rides the sibling
  `httpagent` base. Old-C is new work for opencode, not a re-skin of it.
* Five findings modify what the PLAN must say that [0214] could not yet know:
  1. **The surface grew between the assessment and the engine it will land
     on.** v2.1.4 advertised 149 operations on 124 paths; v2.2.3 advertises 162
     on 134 (probed). New groups include shell, persistent PTY, schedules,
     worktree, websearch, `session.context/environment/move/synthetic/view`,
     `permission.request` listing and instructions entries. Everything the
     PLAN pins (route names, `KnownGoodVersion`) should pin 2.2.3, and the
     catalog of "new in v2 worth adopting" is longer than [0214]'s list.
  2. **`acpagent` declines exactly the `_meta` keys opencode's extensions
     ride on.** `session_info_update._meta` and `usage_update.cost._meta` are
     declined (`internal/provider/acpagent/session.go:2026`, `:1982`), child
     frames outside the live session id are dropped (`internal/provider/acpagent/session.go:1830-1839`),
     and `compaction_update._meta` is declined. Opencode's retry, compaction
     and child-session markers all flow through those keys
     (`packages/cli/src/acp/translate.ts:150-167`,
     `packages/cli/src/acp/compaction.ts:6-44`,
     `packages/cli/src/acp/child.ts:3-66`). The opencode Spec cannot adopt them
     until acpagent grows per-Spec `_meta` handlers — a real dependency-order
     item before [0214] D5/D6.
  3. **ACP lives per-directory.** `session/load`, `session/resume` and
     `session/fork` reject a `cwd` that differs from the session's
     `location.directory` (`packages/cli/src/acp/service.ts:100-101`). One
     `serve --stdio` process served every directory in the 1.x dialect (one
     `cmd.Dir = $HOME` server plus `?directory=`); the ACP path instead needs
     one acp child per project directory, which changes prewarm and turn-latency
     assumptions ([0137]).
  4. **Cold-start on ACP is slow and highly variable.** `initialize` answered
     in 6.7 s on one run and 43.7 s on another; `session/new` took ~45 s
     (probed, no credentials). The ready line itself arrives in ~0.2 s — the
     latency is in the acp child's location/catalog boot. Prewarm and the
     first-prompt deadline budgets must be sized from these numbers.
  5. **The managed background service binds a fixed default port
     (49374 on this platform).** With the owner's real `opencode serve
     --service` already up, CLI commands booted in the isolated scratch home
     still tried to start a managed service on 49374 and hard-failed naming the
     colliding pid and the remedy (`opencode service set port <port>`) (probed).
     [0214] D13's isolation recipe does not cover this: every isolated-home
     live-test environment and any mcremote-adjacent CLI invocation must set a
     distinct service port or it collides with the user's service.
* Scale of the migration, from the package inventory: the OpenCode provider
  package holds ~6,755 production and ~14,249 test lines against the 1.x API
  (`internal/provider/opencode/`), the shared `httpagent` base holds ~9,400
  lines that kilo also uses, and the fixture manifest hard-pins the 1.18.21
  surface at 162 paths / 188 operations / 472 schemas / 89 event types
  (`internal/provider/opencode/surface_contract_test.go:24-31`) — all of it
  replaced or rewritten. The phone/protocol seam needs no new message types
  (`internal/protocol/messages.go:60-176` is provider-neutral); the impact is
  which operations the OpenCode provider can still answer.

## 1. Where the provider stands today (verified)

### 1.1 Launch and gates

* `httpDialect.ServeArgs` (`internal/provider/opencode/http.go:645-651`)
  appends `--pure` when configured — v2 answers `Unrecognized flag: --pure` and
  never starts (0214 table row 1; binary still confirms the flag set is
  `--hostname --port --cors --service --stdio` only, probed via `serve --help`).
* `HealthPath() /global/health`, `EventsPath() /global/event`
  (`internal/provider/opencode/http.go:653-654`). Probed against 2.2.3:
  `/global/health` → `200 text/html`; without credentials `/provider` →
  `200 text/html`; `/api/*` → `401 {"_tag":"UnauthorizedError",...}`;
  `POST /session?directory=` → `405` with an empty body (probed). [0214]'s
  failure table reproduces row for row.
* Boot health is "any 200 is healthy": `internal/provider/httpagent/provider.go:624`
  checks only the status code; `OnHealthy`
  (`internal/provider/opencode/http.go:659-706`) tolerates a non-JSON body by
  design ("keep boot path open"), logs at Debug, and an empty version skips the
  hard floor at `:717`. SSE accepts any 200 with no
  `text/event-stream` check (`internal/provider/httpagent/provider.go:770-771`),
  and the pump reconnects forever with 1–10 s backoff
  (`:749-755`). This is [0214] D12's hardening list, confirmed as absent.
* The engine's stdout is drained to `io.Discard` at boot
  (`internal/provider/httpagent/provider.go:542`), so v2's `server password`
  line (default mode) or its `{"url": ...}` ready JSON (`--stdio`) is never
  seen, and no `OPENCODE_PASSWORD` is ever imported. v2 sends the engine a
  basic-auth requirement that mcremote cannot notice today.

### 1.2 Dialect inventory against the 1.x API

The OpenCode package drives roughly 30 REST paths and two SSE envelope forms:

* Launch/session/stream: `POST /session`, `GET /session/{id}`,
  `GET /session/{id}/message`, `POST .../prompt_async` with
  `{parts, messageID, model, agent, variant}`, `POST .../abort`, `DELETE /session/{id}`
  (`internal/provider/opencode/http.go:1024-1214`), and the v1 event families
  (`message.updated`, `message.part.delta`, `permission.asked/.updated`,
  `permission.v2.asked`, `question.*`, `session.diff`, `command.executed`,
  `todo.updated`, `session.created/updated/deleted/status`, `session.compacted`,
  `session.idle`, `session.error`, … in `HandleEvent`
  `internal/provider/opencode/http.go:1247-1609`).
* Everything else: session ops and diagnostics (`session_ops.go`: rename/fork/
  revert/unrevert/summarize/diff/`GET /vcs`+`/vcs/status`+`/mcp`; note it
  already calls one v2-shaped route (`POST /api/session/{id}/model`, session_ops.go:208-209),
  and a `session_ops.go:177-179` note records that v1's `/summarize` route with
  an `api/session/{id}/compact` shape answered 503 "not available yet" — a good
  snapshot of the 1.x line's transitional shapes), artifacts (artifact.go),
  attachments (attachment.go, 1 MiB frame cap), commands (command.go +
  commandtable.go, the canonical slash-command table of [0023]), modes
  (mode.go: primary agents from `GET /agent`, synthetic `auto` from [0044]),
  model surface (model_surface.go), thinking variants (thinking.go), permission
  funnel (permission.go, dual `permission.v2.*` shapes), questions (question.go),
  share/shell (control.go, operator-gated), todo (todo.go), usage (usage.go),
  resync/tree (resync.go, lifecycle.go), diagnostics surface (diagnostics_surface.go —
  `/skill`, `/lsp`, `/formatter`, `POST /instance/dispose`), workspace browser
  (workspace.go — `/file`, `/file/content`, `/find`, `/find/file`), auth
  (auth.go — `GET /config/providers`, `PUT/DELETE /auth/{id}`, auth.json
  writer), device auth (device_auth.go), upstream switching (upstream.go).
* Fixtures and tests: `testdata/surface-1.18.21` pins the canonical 1.18.21
  surface (162 paths / 188 operations / 472 schemas / 89 event types,
  `surface_contract_test.go:24-31`), `testdata/wire/1.18.26` holds the
  85-frame live SSE capture that backs `KnownGoodVersion`
  (`internal/provider/opencode/version.go:25-28`), and 38 token-spending
  `live_opencode` test functions sit behind the tag (`live_http_test.go` at
  ~1,835 lines plus six more live files). All of it is 1.x evidence.
* Phone seams: the protocol layer is provider-neutral
  (`internal/protocol/messages.go:60-176`; downlink event vocabulary
  `internal/event/event.go:12-128`); the WS handlers call generic capability
  interfaces (`internal/ws/server.go:801-935, 2438-2957`). Nothing there is
  opencode-shaped, so dropping or re-homing surfaces changes what the provider
  answers, not the protocol.
* Scales: `internal/provider/opencode` ≈ 6,755 production / 14,249 test lines;
  `internal/provider/httpagent` ≈ 9,404 lines, shared with kilo (kilo still
  speaks this shape and must not regress ([0214] D3)).

### 1.3 acpagent readiness for an opencode Spec — good bones, known gaps

Opencode has never spoken ACP: there is no ACP code of any kind under
`internal/provider/opencode/` (a tree search returns nothing), and no opencode
ACP Spec exists — the daemon maps `acpagent` onto grok only
(`internal/daemon/daemon.go:87`); kilo shares the `httpagent` base instead.
This section inventories how ready the shared machinery would be to carry this
spec, not what is wired today. What would be reused once implemented:

* The full standard surface opencode v2's ACP emits is already implemented:
  `agent_message_chunk`, `agent_thought_chunk`, `tool_call(+update)`,
  `available_commands_update` (deduped), `plan`, `usage_update`,
  `current_mode_update`, `config_option_update`, `session_info_update`,
  `notice`, `compaction_update`, `compaction_summary_chunk`
  (`internal/provider/acpagent/session.go:1809-2090`), elicitation.form end to
  end ([0191] D1–D3, `internal/provider/acpagent/elicitation.go`),
  `session/new|load` with a 120 s load timeout, permission
  request/response, prompt, cancel, fs callbacks
  (`internal/provider/acpagent/acpagent.go:54-178` Spec surface).
* The declines are the gap, in both directions:
  * `session_info_update._meta` declined (`session.go:2026`) — exactly where
    opencode's compaction marker rides when the client does not advertise
    `session.compaction` (`packages/cli/src/acp/compaction.ts:6-44`).
  * `usage_update.cost._meta` declined (`session.go:1982`).
  * Frames whose session id is not the live id are dropped
    (`session.go:1830-1839`) — exactly where opencode projects child-session
    updates with `_meta["opencode/child-session"]`
    (`packages/cli/src/acp/child.ts:51`).
  * The extension notification method `opencode/session/child_update` is
    simply unknown today; unknown vendor methods reach the extension registry
    (`extensions.go`), which currently only knows grok's `_x.ai/*` surface.
* Version evidence flows the way [0214] D3 wants already: `agentInfo.version`
  at initialize with a `_meta.agentVersion` fallback
  (`internal/provider/acpagent/version.go:11-27`), one warning per mismatched
  `KnownGoodVersion`, never a refusal (probed live: `initialize` answered
  `"agentInfo":{"name":"OpenCode","version":"2.2.3"}`).
* Permission selection on ACP maps cleanly onto the auto-mode work: opencode's
  options are exactly `once→allow_once`, `always→allow_always`,
  `reject→reject_once` (`packages/cli/src/acp/permission.ts:39-74`), so
  kind-first automatic selection ([0214] D9) also retires the substring defect
  A2 named in the SDK's consumer report.

## 2. What the installed engine actually is (probed, v2.2.3)

### 2.1 Identity and install channel

* `/Users/<user>/.opencode/bin/opencode`, arm64 Mach-O, `opencode --version` →
  `opencode v2.2.3` (probed). The sibling checkout reads 2.2.2 in every
  package but its committed OpenAPI equals the live one, so the checkout is
  release-adjacent dev code rather than a pinned release snapshot.
* The machine also runs the owner's managed background service
  (`opencode serve --service`, up since 2026-10-18, real home) and one plain
  `opencode serve` on a fixed port from 2026-10-24 — both pre-existing and both
  left untouched throughout these probes.

### 2.2 CLI surface (v2.2.3, probed)

Subcommands: `upgrade|update`, `uninstall`, `acp [--login]`, `api`,
`debug agents|config|heap-dump|paths`, `auth list|login|logout|export|import|switch`,
`mcp list|add|auth|logout`, `plugin list|add|check|update|remove`, `models`,
`stats`, `mini`, `run`, `session list|delete|export|import`,
`service start|restart|status|stop|get|set|unset`, `reload`, `pair`, `serve`.
Globals: `--standalone`, `--server <url>`, `--auto` (plus hidden
`--yolo`/`--dangerously-skip-permissions` aliases per
`packages/cli/src/commands/commands.ts:36-46`), `--continue/-c`,
`--session/-s`, `--prompt`. `serve` takes `--hostname --port --cors (repeat) --
service --stdio` (probed; no `--password` flag; no `--pure`).
`run --format json` and `--model provider/model#variant`,
`--agent`, `--file`, `--title`, `--thinking` (probed). `api` accepts an OpenAPI
operation id or `METHOD path` with `--data/--header/--param` (probed). `pair
--remote --yes` exposes the server through OpenTunnel (probed help text).

Two operational wrinkles observed while probing:

* Parallel invocations of different `opencode --help` runs hung past two minutes
  at two different subcommands before any output; run sequentially they all
  returned instantly. Not diagnosed further — reported only as "CLI invocations
  are not safe to parallelize on first boot of an empty home."
* `auth list`, `models` and `debug agents` inside an isolated home hit the
  **fixed default managed-service port** described above and failed with
  `Managed service port 49374 on 127.0.0.1 is already in use by PID …
  Configure another port with 'opencode service set port <port>'` (probed,
  verbatim modulo redaction). After `service set port 42901` in the isolated
  state, all three worked, listing e.g. locally discovered ollama models with
  no credentials configured.

### 2.3 HTTP surface (probed against the isolated serve)

* OpenAPI 3.1.0 at `/openapi.json`: **134 paths, 162 operations** (probed;
  confirmed again from the checkout's committed document). 46 of the 162 are
  `experimental.*` (counted from the checkout). The information title is
  `opencode HttpApi`.
* Groups present: session (39 stable + 9 experimental), integration (12),
  pty (7), persistent-pty experimental (11), schedule (6), shell (5),
  credential (5), vcs (6), worktree (4), permission (4 incl. `request`
  listing and `saved` CRUD), plugin (4), generate (4), fs (6), debug (3),
  server (3 incl. pair/connect), project/config/model/provider/mcp (2 + 6 for
  mcp.experimental), voice (2), websearch (2, incl. `query`), rpc (1),
  reference (1), form (1), command (1), skill (1), event (1), usage (1),
  migration (1), media (1), location (2) (counts from the checkout's committed
  document; the live document matched on size).
* Not in the surface (probed absence): LSP, formatter, share, session todo.
  [0214]'s three named regressions plus the fourth (todo) all stand.
* `GET /api/credential` is documented to return stored credentials **including
  secret values** (`packages/protocol/src/groups/credential.ts:14`). [0214] D2
  "never" stands, and the control plane's phone-auth path must be built so it
  cannot ever need it.
* Auth: Basic `opencode:<password>`; username hardwired
  (`packages/server/src/auth.ts:20`; "The username is always `opencode`",
  `services/www/src/docs/content/migrate-v1.mdx:614`); password from
  `OPENCODE_PASSWORD` (legacy, preferred) or `OPENCODE_SERVER_PASSWORD`
  (`packages/cli/src/env.ts:10-13`); both are deleted from the server's own env
  in stdio mode (`packages/cli/src/server-process.ts:87-90`). Pairing links
  issue HMAC tokens (30-day TTL, `packages/server/src/auth.ts:16,40-61`).
* Location scoping: deepObject query `location[directory]=`, then the
  `x-opencode-directory` header (URL-encoded), then the server process cwd
  (`packages/server/src/location.ts:71-79`). Probed: the header drove every
  location-scoped read; `_api/info` returns `{version, pid, hostname, urls,
  paths}` plus `capabilities:{persistentPty:true}`; `GET /api/location` returns
  `{directory, project:{id, directory, canonical}}`;
  `POST /api/location/reload` rebuilds all locations.
* Envelopes (probed): `{location:{directory}, data}` for location-scoped
  GETs (`/api/provider`, `/api/command`, `/api/model`, `/api/agent` once it
  stops 503ing); `{data}` / `{data, cursor:{previous,next}}` for global ones
  (`/api/session` list is pageless-empty on a new home and `/api/session/active`
  returns a **map**, not a list, at this version). Errors are typed
  (`{"_tag":..., "message":..., "kind":"Query"|"Params"}` on schema
  rejections — probed both); the server log records `schema rejection` WARNs.
* Session lifecycle without a prompt (probed): `POST /api/session` with body
  `{"location":{"directory":...}}` → session `{id:"ses_…", projectID, cost,
  tokens{...}, time{created,updated}, location{directory}}`; `GET /api/session/{id}`;
  `GET /api/session/{id}/message` → `{data:[],cursor}`; `DELETE` → `204`.
  `PATCH /api/session/{id}` takes `title|metadata|permissions|archived`
  (record/source). Prompt is `POST /api/session/{id}/prompt` with
  `{text|files|agents|skills|metadata|delivery|resume}` and async admission
  (record/source; unprobed — no prompt was sent).
* Event stream: `GET /api/event` → `200`, immediately `data:
  {"id":"evt_…","type":"server.connected","data":{}}`, then `: heartbeat`
  comments every 15 s (`packages/server/src/handlers/event.ts:23`), frames flat
  with no `properties` wrapper (`packages/server/src/event-feed.ts:29-31`),
  optional `?types=` filter, per-subscriber queue capacity 4,096 with
  drop-and-fail overflow semantics (`event-feed.ts:8,66-69`). Probed idle;
  during-call frames were not captured (no prompt), which is fine — the schema
  is source-checkable.
* Replay: `GET /api/experimental/session/{id}/log` exists with `after`/`follow`
  (`packages/server/src/handlers/session.ts:598-606`); probed only its input
  validation (`follow` must be the exact strings `"true"|"false"`, sessionID
  must start `ses`). Whether it can carry turn content to rebuild a transcript
  after a disconnect remains the same open question [0214] recorded at its
  Confirmation list; a first-party client uses `?types=` + `after=0` on the
  live stream instead (`packages/protocol/src/groups/event.ts:38-53`).

### 2.4 `serve --stdio` lease (probed)

`sleep N | opencode serve --stdio --port 0` printed exactly one line,
`{"url":"http://127.0.0.1:<port>"}`, and exited 0 when stdin reached EOF; with
`OPENCODE_PASSWORD` in the child env, no password line is printed
(`packages/cli/src/server-process.ts:208-209` prints the password only in
foreground mode without one). Kill-after-lease-closed is SIGTERM then force
after 3 s (`packages/cli/src/services/standalone.ts:30-31`); stdout must be
drained past readiness to avoid EPIPE (`standalone.ts:41-48`). Matches
[0214] D2 exactly.

### 2.5 ACP (probed through a stdio JSON-RPC harness, no prompt)

* `initialize` (client capabilities incl. `elicitation.form` and
  `_meta["opencode/child-session-updates"]`) →
  `protocolVersion: 1`; `agentCapabilities: loadSession, mcpCapabilities
  {http:true,sse:false}, promptCapabilities {image, embeddedContext},
  sessionCapabilities {additionalDirectories, close, delete, fork, list,
  resume}, _meta["opencode/child-session-updates":true]`; auth method
  `opencode-login`; `agentInfo {name:"OpenCode", version:"2.2.3"}` (probed;
  registry at `packages/cli/src/acp/agent.ts:68-119`).
* `session/new` → result keys `{sessionId, configOptions}` — the config
  option bundles (model/effort/mode) now arrive **in the result**
  (`packages/cli/src/acp/service.ts` handshake; category sources at
  `packages/cli/src/acp/config-option.ts:22-68`), followed by
  `available_commands_update` with `init, review, goal, compact` (probed).
  `goal` is new in 2.2.1 (CHANGELOG v2.2.1) and postdates [0214]'s
  `[init, review, compact]` — the canonical command table and the dialect's
  "no goal" notes must be rewritten ([0023] wiring, [0179]).
* Latency (probed, all locked to first-touch catalog work): `initialize`
  6.7 s on one run, 43.7 s on another; `session/new` ~45 s. An `opencode acp`
  child that receives EOF before its own child server is ready dies silently
  with no output and exit 0 (probed three different ways) — a client harness
  must hold stdin open through the handshake; acpagent already treats engines
  gently here (cancel/close methods, bounded `loadTimeout`), but the launcher
  must not use short-lived stdin.
* Update vocabulary the translator emits (`packages/cli/src/acp/translate.ts`,
  source-verified): text/reasoning chunks, `tool_call`/`tool_call_update`
  (with a live `error` state), `session_info_update` carrying
  `_meta["opencode/retry"] {attempt, nextRetryAt, error}` and cleared with
  `null` on the next step, `config_option_update`, `usage_update`,
  compaction updates/summary chunks when `session.compaction` is advertised or
  an `_meta["opencode/compaction"] {status, messageId, reason, error?}`
  marker when it is not, child-session projection with `_meta
  ["opencode/child-session"] {id, parentID, depth, title}` and
  `<childSessionId>:`-prefixed tool-call ids, and the opt-out for
  `opencode/session/child_update` notifications. Stop reasons:
  `cancelled|max_tokens|refusal|end_turn`; failure mapping
  `provider.auth→auth_required` else `ServiceFailure`.
* Forms: `elicitation/create` only with the `elicitation.form` capability
  advertised, only for metadata kinds `question` and `websearch.provider`
  (`packages/cli/src/acp/elicitation.ts:22`), hidden fields answered from
  defaults, decline/cancel maps to `session.form.cancel` — compatible with
  mcremote's funnel ([0191]) which already supports this capability.
* Load/resume/fork audited above (§3 finding 3 — the cwd equality rule).

### 2.6 Providers, credentials, catalog (no-credential behavior)

* With no credentials: `GET /api/provider` lists the bundled OpenCode free
  provider and any locally discovered servers (probed: the owner's local
  ollama showed up — an empty home still advertises real provider rows);
  `/api/model/default` returns a model with `variants [low..max]`,
  `enabled`, `status`, `limit` (probed); `GET /api/agent` returned a
  persistent `503 {"_tag":"ServiceUnavailableError","message":"Location
  plugins did not finish activating","service":"plugin"}` on the used-free
  home for minutes (probed repeatedly). That 503 is the plugin-gate surface
  the v2.2.0 CHANGELOG ("Wait for plugins to finish reloading before each
  session step and tool call", and "Wait for plugins before returning agent
  and provider lists") states as intent. A control-plane client needs 503
  handling with retry, not error-out; mcremote's boot health path would
  currently treat it as healthy and drop the catalog.
* Catalog breadth: `GET /api/agent` en-route catalogs burst as `*.updated`
  events (`provider.updated`, `model.updated`, `agent.updated`, …) — matching
  on an engine log read during a session lifecycle probe (probed — event types
  in the isolated engine's own log); the phone-side grouping design ([0214] D7)
  is what keeps a 1,000-plus flat model list off the wire when credentials
  exist.

### 2.7 Two records-keeping notes

* The repository-root `opencode.json` in this project is the OpenCode engine's
  own repo-local configuration (schema `https://opencode.ai/config.json`, agent
  rules and a bash allow/deny table). It governs the opencode CLI when it is
  pointed at this repo; it is not daemon config and needs no migration in the
  v2 work (it is simply ignored by mcremote).
* `scripts/capture-wire/main.go` and the `surface-*` fixtures are today's
  pipeline for pinning engine surfaces ([0112]-era pipeline); the v2 PLAN
  should reuse it for a 2.2.3 manifest instead of the retired 1.18.21 one.

## 3. Deltas between the assessment in [0214] and v2.2.3

What the PLAN needs that the record does not yet contain:

| Δ | Evidence | Effect on the PLAN |
| --- | --- | --- |
| Surface 124/149 → 134/162; 46 experimental ops | probed on `/openapi.json`, matched by the checkout's committed document | Pin `KnownGoodVersion` to 2.2.3 (or ≤ it) and pin the surface contract from a fresh `capture-wire` run; [0214]'s audit table needs the shell/pty/schedule/worktree/websearch/rpc/reference/context/environment/move/synthetic/view rows added |
| `session/new` returns `configOptions` in the result | probed | The ACP catalog arrives on the session-creation round trip; the 1,000+-option wire-size gate (G6 with [0181]) now measures the session/new result plus later `config_option_update`s |
| `goal` joined the command set; `/schedule` and `image_generate` are opt-in-in-CI features | probed; CHANGELOG 2.2.1 | Canonical command table ([0023]/[0179]) and the dialect's KindNone reasons get new rows; `run`-side `--auto` global exists too |
| Plugin-activation gate behind `GET /api/agent` (`503 ServiceUnavailableError`) | probed on an empty home | Control-plane health/catalog code must retry on 503; boot content-type checking (D12) also catches it |
| Managed service fixed default port 49374 collides across two "worlds" on one host | probed (verbatim error text) | D13's isolation recipe needs a `service set port` step (or a deliberate policy that CLI boot is never used in tests), and `doctor` may want to report a collision cleanly |

Also confirmed with no change: auth model, envelope heights, event feed
formatting/overflow numbers, `--stdio` lease mechanics, ACP capability set,
`agentInfo.version`, the absent LSP/formatter/share/todo routes, and the
`/api/credential` secrets warning — all probed or source-verified and stable
against the 2.1.4 facts.

### What the probes did and did not cover from [0214]'s gate list

Covered by probes (no prompt needed): ready line shape, exit-0-at-EOF lease,
`401` without auth, JSON `/api/info`, the location parameter and header paths,
`initialize` capabilities incl. `session/new` options and the
`child-session-updates` `_meta`, cold-start latency bands, the fixed
service-port hazard. Not covered, still waiting on [0214] G1–G6 with quota,
a turn, or other hosts: a real ACP turn (G1), `session/load|resume` after a
restart (G2), the Linux/Windows lease behavior (G3), credential-removal
propagation (G4), cross-process revert/shell against an engine-owned session
(G5), the config-option wire size with a real credential set (G6).

## 4. Capability parity matrix (dialect ↔ v2.2.3)

mcremote surface (dialect method) → v2.2.3 HTTP route(s) → v2.2.3 ACP
mechanism → assessment. "CP" = the planned lease-bound control plane.

| mcremote surface | v2.2.3 HTTP | v2.2.3 ACP | Assessment for [0214]-C |
| --- | --- | --- | --- |
| Session create/list/load/resume (`Create`, `ListAgentSessionsLive`, ...) | `POST/GET /api/session` (envelope, body location), `parentId`+cursor on list | `session/new` (pin `cwd` to `location.directory`), `session/list` | Both fine; ACP needs per-directory children |
| Streaming text/thinking/tool parts (`HandleEvent`) | all renamed/reshaped SSE families (`session.text.*`, `session.reasoning.*`, `session.tool.*`) | `agent_message_chunk`, `agent_thought_chunk`, `tool_call(+update)` | standard kinds already implemented in acpagent (grok exercises them today) — nothing to write beyond the Spec |
| Abort (`Abort`) | `POST .../interrupt` ({resume}) | `session/cancel` | status quo |
| Permissions (`RespondPermission`, auto modes) | `session.permission.reply` `{decision: once\|always\|reject}`; rulesets; saved rules | `session/request_permission`, options exactly `allow_once\|allow_always\|reject_once` | better than v1: kinds travel with the options |
| Questions (`RespondQuestion`) | `session.form.{list,create,get,cancel,reply}` | `elicitation/create` (form), `question`/`websearch.provider` metadata only | would be handled by the shared elicitation funnel ([0191], grok-tested); opencode's server-side metadata filter rules pre-exist in the engine |
| Modes (`SetMode`) | `session.switchAgent` — engine does not validate agent ids (0214 probe) | `mode` config option + `session/set_mode` | ACP preferred; HTTP needs client-side validation |
| Model (`SetModel`) | `session.switchModel` | `model` config option, `#variant` tolerated | both fine |
| Thinking (`SetThinkingLevel`) | variant on prompt/model switch | `effort` (`thought_level`) option — only when the model has variants | ACP only; hide the control on variant-less models |
| Commands (`ListCommandsLive`, `submitCommand`) | `GET /api/command`, `POST .../command` `{name, text}` | `available_commands_update` + `/name` forwarding | ACP fine; HTTP dedupe against catalog (`commanddedupe_test.go` row) |
| Subagent tree (`ConfirmTreeIdle`, child suppression) | `GET /api/session?parentID=`, `GET /api/session/active` (map!) | `opencode/session/child_update` + `_meta["opencode/child-session"]` projection | acpagent extensions first (§1.3) |
| Compaction (`Compact`) | `POST .../compact`; `session.compaction.*` | `compaction_update`/`compaction_summary_chunk` (capability) or `_meta["opencode/compaction"]` | both; `_meta` form needs acpagent work today |
| Usage and cost (`emitUsage`, 0189 total) | `session.step.ended` tokens/cost; `session.usage.updated` | `usage_update` (cost `_meta` declined today) + prompt-result usage | NP; cost `_meta` handler adds provider-reported rows (2.2.0 "record provider-reported session costs") |
| Retry / limit cards | `session.retry.scheduled` {attempt, at, error}; `retryAfterMs` on session errors (2.1.2) | `_meta["opencode/retry"]` on `session_info_update` + prompt result | acpagent handler needed (D6) |
| Fork (`Fork`) | `POST .../fork` (`before` message id) | `session/fork`, cwd-locked | both |
| Rename (`Rename`) | `PATCH /api/session/{id}` | none | CP only |
| Revert/undo/unrevert (`Revert`, `UndoLast`, `Unrevert`) | `revert/stage`, `/commit`, DELETE revert | none | [G5] decides whether CP works against an engine-owned session; otherwise a named regression (D11) |
| Shell (`Shell`) | `POST .../shell`; new `/api/shell` CRUD + `pty` routes | none | D11 same as revert; CP-enabled if G5 holds |
| Share (`Share`, `Unshare`) | — no v2 route | none | named regression |
| Todo panel (`handleTodoUpdated`) | experimental tool only, no read API (experimental config) | none | named regression |
| Diff (`Diff`) | `GET .../diff` (per-turn structured) | tool-call diff content only | CP preferred |
| Diagnostics (`mcp/skill/lsp/formatter`) | `GET /api/mcp`, `GET /api/skill`, `GET /api/plugin`(+gui); **no LSP/formatter routes** | — | two names dropped from the diagnostics list (D10) |
| Workspace browser | `GET /api/fs/list\|read\|find` (+ `fs.watch` 24 h, raw reads with ETag/range) | none | richer than v1 (`/file/content`); no full-text grep route — D10's bounded local search decides |
| Phone auth (`SetCredential`, device/auth flows) | `integration connect key\|oauth\|command\|external` + `credential activate/PATCH/DELETE`; **never** `GET /api/credential` | terminal `opencode-login` only | CP only; keep D8's restart-on-remove until G4 retires it |
| Active upstream / drill-down (0079) | `GET /api/provider` `activation`, integration connections | the `model` option lists every enabled model | grouping happens on CP; never send the raw 1,000+ list to the phone |
| NEW v2 features worth adopting (beyond parity) | `experimental.schedule.*` (6), `experimental.generate.{text,image,decisions}`, `experimental.media/{hash}`, persistent PTY set + `/api/pty`, `session.context`, `session.environment`, `session.move`, `session.synthetic`, `session.view`, `session.background`, `session.generate`, `experimental.usage.limits`, `experimental.session.wait`, instructions entries, worktree CRUD, `websearch.query`, saved-permission rulesets | child-session updates, effort, compaction lifecycle, retry markers, elicitation forms | each gets its own record when adopted; incompatible with "no silently dropped features" unless the PLAN names them |

## 5. What must be built — work breakdown (ordered, with the dependency rationale)

The ordering follows the dependency chain found in §1.3/§3 rather than feature
convenience; each item names the record that owns the decision.

**A. Shared hardening first (`internal/provider/httpagent` + shared provider
plumbing; kilo benefits too — [0214] D12).**

1. Health/SSE content-type enforcement (`internal/provider/httpagent/provider.go:624,770-771`):
   `application/json` for JSON probes, `text/event-stream` for the stream; a
   mismatch is a loud error with the body's first bytes. Done once, it turns
   the next silent break (opencode or kilo) into one log line.
2. Health-body gate: the JSON body must decode a version; empty versions stop
   skipping `CheckMinVersion` (`internal/provider/opencode/http.go:699-724`).
3. Exit-status classification: v2 SIGTERM-bounced engines exit 130; today
   classify-anything-but-that is implicit. Fold it into `serverDied`
   (`internal/provider/httpagent/provider.go:693-721`).
4. Optional, small: a password-env capability on the launch path so a
   dialect can pass an engine a secret through the environment and learn its
   ready line — the general form of [0214] D2's credential plumbing.

**B. acpagent extensions (before any opencode Spec — §1.3 findings).**

1. Per-Spec `_meta` annotation handlers for `session_info_update._meta`,
   `usage_update.cost(_meta)`, `compaction_update._meta` and projected
   `tool_call_update` meta (`internal/provider/acpagent/session.go:1982-2049`) —
   grok's notification-registry pattern (`internal/provider/grok/grok.go:92-138`)
   extended to annotations.
2. Vendor notification method registration for
   `opencode/session/child_update` (`extensions.go`), with the child-session
   projection key handling in §1.3 and the current drop-at-source behavior
   (`session.go:1830-1839`) replaced by the Spec's opt-in.
3. Conformance rows (`internal/provider/acpagent/conformance_test.go`) for
   each extension key, so a spec cannot claim support silently.

**C. The OpenCode ACP Spec (D1).** New `internal/provider/opencode` shape or
sibling package over acpagent, modeled on the grok Spec
(`internal/provider/grok/grok.go:49-139`): argv (`opencode acp`),
`ClientCapabilities` (fs read/write, terminal, elicitation.form,
session.compaction, `_meta opencode/child-session-updates`, auth.terminal),
auth method taxonomy (opencode-login only, headless-glaring), model/mode/effort
config-options synonyms, canonical command table rewrite (adds `goal`,
removes v1-only rows; [0023] conformance rows
`internal/command/conformance_test.go:50-80`), static modes, no
`SetCredential` (ACP has no auth surface beyond terminal login), static model
seeds. Design constraint from §2.5: one acp child per project directory
(cwd-locked loads), so the Spec's process model is per-directory, not
per-engine.

**D. The control-plane engine (D2).** A new module (probably
`internal/provider/opencode/control/`) that:

1. Launches `opencode serve --stdio --port 0` with a per-launch random
   password in `OPENCODE_PASSWORD` (env only, never argv/logs/persisted),
   learns `{url}` from one stdout JSON line, drains stdout thereafter,
   treats stdin EOF as the lease, sends SIGTERM→kill with `exit 0`/`130`
   accepted as orderly (probe facts in §2.4).
2. Speaks Basic auth (`opencode:<password>`; legacy env name preferred by the
   engine — the client only needs the value it generated), deepObject
   `location[directory]` AND the `x-opencode-directory` header on every
   location-scoped call, and body `location.directory` on `session.create`
   shapes.
3. Starts on demand (phone-auth used, workspace opened, diagnostics/diff,
   grouping) and idles out; never calls `GET /api/credential`
   (`packages/protocol/src/groups/credential.ts:14` — it returns secrets).
4. Handles the service-neutral 503 gateway (`GET /api/agent`) with retry, and
   `{data:{}}` map-shaped session-active responses (§2.3).

**E. Feature re-homing (the CP menu).** Phone auth on
`integration.connect*` + `credential.*` (key/oauth/command/external; the
auth.json fingerprint retires — v2 keeps credentials in its database, with
`auth.json` accepted for import only per [0214]); provider-activation
grouping for the drill-down ([0079]); workspace on `fs.list/read/find`
(D10's text-search decision: bounded local search or drop); diagnostics on
`mcp/skill/plugin/vcs/skill-activated/skill-refresh` (`/api/vcs`,
`session.diff` for diffs); LSP/formatter names removed from the surface; forms
global list; the inbox/steer/queue and `session.background`/`session.generate`
surfaces if adopted.

**F. Retirement of the 1.x dialect.** Delete or tombstone
`httpDialect`/`SSE`/`resync`/`tree-confirm`/`question`/`todo`/`share`/`device`
1.x code paths (the ~6.7k/14.2k line block; [0214] Consequences already names
this), with the config layer's `RetiredTransport` validation pattern for
`pure`, `session_tree`, `stream_coalesce_ms`, and the [0214]-named dropped
`allow_remote_share` — validate errors name the key and the record
(`internal/config/config.go:544-603` carries the keys today;
`configs/config.example.yaml:141-179` carries the block).

**G. Versioning/doctor/engines.** `MinVersion`/`KnownGoodVersion` move to the
[0214] D3 policy (`≥ 2.1.0` floor, `KnownGoodVersion 2.2.3` after the fresh
probe — the D3 floor was written when the engine was 2.1.4; re-assert or
amend on the PLAN); 1.x engines refused with the install pointer
(`https://opencode.ai/v2/install`); `doctor` learns `/api/info` +
`agentInfo.version`, stops reading the now-stale auth.json fingerprint
(`internal/provider/opencode/auth.go`, `internal/provider/credstore/credstore.go`),
and reports the service-port collision cleanly (a 2.2.3-era hazard,
§2.2); `mcremote engines` (`internal/cli/engines.go:17-20`) learns to name
ACP children and lease-bound CP engines distinctly.

**H. Tests and fixtures.** The 1.18.21 manifest
(`internal/provider/opencode/testdata/surface-1.18.21`) and
`wire/1.18.26` fixtures retire with the dialect. New:

* a 2.2.3 surface manifest from `make`-integrated `scripts/capture-wire`
  (162 ops / 134 paths with the 46-experimental split),
* recorded ACP frames for child updates, retry/compaction `_meta`,
  permission kind selection, config-option grouping, elicitation form
  (unit tests, no quota),
* `live_opencode` rewrites for the [0213]-style `--stdio` lease, the
  isolated-service-port, 503-gateway-retry, body-location creates and
  envelope paging, with the [0214] D13 env recipe plus the service-port
  isolation finding (§2.2),
* the no-prompt probe facts from this report pinned as
  live-tagged asserts (0214's own Confirmation list).

**I. Phone/protocol seam.** No new message types required; the deltas are
which operations the OpenCode provider answers and one new OpenCode-only
wire row if `websearch/schedule` features get adopted (each with its own
record). Mobile docs touchpoints that document today's OpenCode capability set
need a sweep when the retirement lands (README provider rows at README.md:238+
and README.md:828-855; mobile docs' opencode vendor rows).

**J. Records.** [0214] gains its PLAN against 2.2.3 (with the §3 deltas and
the G-list evidence on file) and keeps its `status: proposed` until the owner
word; [0217]'s pin row ("opencode 1.18.26 → current v2.2.2") updates to the
fresh probe's version; [0112]/[0021]/[0020]/[0019] remain historical context
cited, not edited.

## 6. Open questions (stated, not papered over)

1. Whether `GET /api/experimental/session/{id}/log` can replay turn content
   (0214's 2.1.4 probe and this report's validation-only probes both left it
   unverifiable without a prompt or a credential).
2. Whether a `503 ServiceUnavailableError` on `/api/agent` persists on a
   credentialed, plugin-populated home or only on a plugin-less one (probed on
   the empty home only; 2.2.0 CHANGELOG says the engine waits, so a bounded
   retry is right either way).
3. Whether the control plane's revert/shell routes can act on a session the
   ACP engine owns in another process (G5) — until probed,
   D11's named regressions stand.
4. Whether the Windows/Linux behavior for the `--stdio` lease, the exit
   statuses and the fixed service port match macOS (G3).
5. The exact 2.2.3 changelog body (the checkout's CHANGELOG tops out at
   2.2.2; the installed binary self-reports 2.2.3 — patch-level, but the PLAN
   should confirm against upstream at pin time).
6. Whether the adopt-existing-id semantics of `POST /api/session` (id
   optional) let mcremote create-adopt its own stable ids, which would
   simplify phone-side id bookkeeping for the CP.

## 7. Method notes and limits

* All probes ran in a scratchpad outside both repositories, with engine
  writes contained (isolated `HOME` and all `XDG_*` roots; autoupdate and
  filewatcher disabled). The two server ports used (41933-41935, dynamic) and
  the fixed service port were the only listeners; the owner's own two
  long-running `opencode serve` processes were identified by environment and
  left running untouched.
* No prompt was sent anywhere; no provider credentials were present; no
  user data was read or written.
* Probe artifacts (help captures, `openapi.json`, SSE samples, ACP harness
  transcripts, the service-collision stderr) live in the same scratchpad for
  reference during the PLAN; they are not committed, matching [0214]'s
  practice for its own probe scripts. Stable facts should graduate into
  live-tagged tests as outlined in §5-H.
* One source-vs-live nuance to keep honest: the second agent that surveyed
  the opencode checkout counted 133 paths where this report counts 134 (both
  agents counted the live document identically at 134/162; the
  recount method — every path key with a standard-method operation —
  reconciles them). Where a number matters to a gate, recount with
  `scripts/capture-wire` output rather than trusting either count.

## Related records

* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](../decisions/0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md) — the proposed decision this report supports; its own 2.1.4 probe.
* [0112-MADR-opencode-1.18.21-surface-parity.md](../decisions/0112-MADR-opencode-1.18.21-surface-parity.md) and [0112-PLAN-opencode-1.18.21-surface-parity.md](../decisions/0112-PLAN-opencode-1.18.21-surface-parity.md) — the 1.x surface-pinning pipeline this report's §5-H refits.
* [0217-MADR-update-every-pin-to-its-current-release.md](../decisions/0217-MADR-update-every-pin-to-its-current-release.md) — carries the "opencode 1.18.26 → 2.2.2 / subject of 0214" pin row.
* [0019](../decisions/0019-MADR-opencode-process-management-plan.md), [0020](../decisions/0020-MADR-opencode-session-tree.md), [0021](../decisions/0021-MADR-opencode-http-api-coverage.md), [0034](../decisions/0034-MADR-opencode-tool-stream-fidelity.md) — the 1.x HTTP/SSE lineage being retired.
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](../decisions/0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md), [0208-MADR-make-the-provider-layer-agent-neutral.md](../decisions/0208-MADR-make-the-provider-layer-agent-neutral.md) — the agent-neutral direction; acpagent as the session transport.
* [0191-MADR-answer-agent-elicitation-forms-on-the-phone.md](../decisions/0191-MADR-answer-agent-elicitation-forms-on-the-phone.md) and [0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md](../decisions/0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md) — elicitation funnels and the child-session deferral opencode v2 now satisfies.
* [0079-MADR-provider-model-drill-down-picker.md](../decisions/0079-MADR-provider-model-drill-down-picker.md), [0074-MADR-remote-provider-auth-from-phone.md](../decisions/0074-MADR-remote-provider-auth-from-phone.md), [0083-MADR-provider-auth-activation-and-layout-gaps.md](../decisions/0083-MADR-provider-auth-activation-and-layout-gaps.md) — the phone auth/catalog flows the control plane re-homes.
* [0023-MADR-canonical-slash-commands.md](../decisions/0023-MADR-canonical-slash-commands.md), [0179-MADR-gobble-native-acp-provider.md](../decisions/0179-MADR-gobble-native-acp-provider.md)-era command-table conformance and [0177-REPORT-flutter-android-client-assessment.md](../../apps/mobile/docs/reports/0177-REPORT-flutter-android-client-assessment.md) as the mobile-side knowledge base for what the phone expects.
