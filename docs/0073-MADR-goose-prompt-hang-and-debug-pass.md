# MADR 0073: Goose prompt hang (silent 3600 s provider backoff) + codebase debug pass

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: **Findings recorded 2026-08-05; remediation not started.**
  Root cause of the reported symptom is verified live end-to-end (§1–§2).
  Audit findings in §4–§7 come from a parallel code audit; each carries
  its reporter's severity/confidence ranking and has **not** been
  independently re-verified unless marked ✅.
- **Date**: 2026-08-05
- **Deciders**: Project Owner
- **Scope**: Reported symptom: *"start a new goose session on the phone
  app, send a prompt, nothing comes back — it just hangs."* Plus a
  codebase-wide bug/gap sweep (Go daemon, relay, providers, session/event
  layer, CLI/config/auth/update packages, Flutter app UI subset).
- **Method**: live host forensics (`mcremote.err.log`, goose server
  logs), isolated repro daemon (`dev` build of HEAD `19614ed` on
  `127.0.0.1:7799`), headless `scripts/smoke-protocol.sh` repro,
  `debugpprof` build + goroutine dump mid-hang, and a raw ACP
  WebSocket probe speaking directly to `goose serve` with mcremote
  entirely out of the loop. Code audit by parallel review agents over
  `internal/*`, `cmd/*`, and part of `apps/mobile`.
- **Related**:
  [0072-MADR-phone-reconnect-and-provider-timeout-incident.md](0072-MADR-phone-reconnect-and-provider-timeout-incident.md)
  (F8 "sticky running" is this same product surface),
  [0068-MADR-protocol-v2-reconnect-resilient-transport.md](0068-MADR-protocol-v2-reconnect-resilient-transport.md)
  (seq/resume contract referenced by §5),
  [0069-MADR-macos-permissions-and-sandbox-parity.md](0069-MADR-macos-permissions-and-sandbox-parity.md)
  (goose mode defaults referenced in §1.4).

## 0. Executive summary

The goose hang is **not an mcremote wiring bug**. Goose's active model
provider (`opencode_go`, model `minimax-m3`) is over its weekly usage
limit; every LLM call returns **HTTP 429 `GoUsageLimitError` — "Weekly
usage limit reached. Resets in 4 days."** Goose then **silently backs
off 3600 seconds before retrying** (`goose_provider_types::retry`,
"Backing off for 3600s before retry"), emitting **no `session/update`
and no `session/prompt` error** for that hour. mcremote's turn is
deliberately cancel-proof (`context.WithoutCancel` +
`wsFramer.sendRequest` blocking until the agent answers), so the phone
sees `status=running` and then nothing — exactly the reported symptom.

Everything in the mcremote chain up to that point works and was verified
live: engine boot, `initialize`, WS dial, `session/new`,
`session/set_config_option`, prompt acceptance, event fanout of
`user_message` + `session_status:running`.

The debug pass still matters: it found (a) observability gaps that made
this hour-long stall invisible (§3), and (b) a ranked backlog of real
bugs in the transport, session/event, and CLI layers — several of which
can *independently* produce "prompt sent, nothing streams back" (§4–§5).

**Immediate operator fix**: switch goose's active provider/model (e.g.
`goose configure`, or per-session model select from the phone), or pay
down the opencode quota. **Highest-value code fixes**: surface provider
retry/backoff as a turn event (F1), add prompt-dispatch logging to
acphttp (F2), and triage §4 F-T1/F-T2 and §5 F-S1/F-S2/F-S3.

## 1. Verified repro chain (all live, 2026-08-05)

### 1.1 Live daemon log shape

`~/Library/Logs/mcremote/mcremote.err.log` (v0.8.0.7.g19614ed): goose
sessions are created (`provider=goose agent_session_id=20260805_7/8/9`)
but — unlike opencode, which logs `opencode prompt_async … ok=true` —
**no prompt-dispatch line ever appears for goose**, and no goose events
follow. Codex, grok, and opencode sessions on the same build create
sessions *and* process prompts normally.

### 1.2 Headless repro without the phone

Isolated daemon (HEAD, `127.0.0.1:7799`, goose only) +
`scripts/smoke-protocol.sh -provider goose`:

```text
session.create → ✓ (remote_commands, session_mode, session_config,
                    session_capabilities events arrive)
session.prompt → ✓ "prompt accepted"
events:          user_message, session_status=running
then:            NOTHING for the full 60–90 s observation window
```

The phone app is exonerated: the hang reproduces with the repo's own
smoke client.

### 1.3 Goroutine dump mid-hang (debugpprof build)

One turn goroutine parked exactly where designed:

- `session.runTurn` → `wsFramer.sendRequest("session/prompt", …)`
  blocked in its response `select` — `internal/provider/acphttp/ws.go:90`,
  called from `internal/provider/acphttp/session.go:440`.
- `readPump` healthy in `ws.Read` (`ws.go:202`) — the engine socket is
  alive; goose simply never answers.

`session/new` and `session/set_config_option` had round-tripped over the
same framer moments earlier, so request/response correlation works.

### 1.4 mcremote fully out of the loop

A raw probe (fresh `goose serve --dangerously-unauthenticated`, HTTP
`initialize`, WS `session/new` → `session/prompt`, printing every
inbound frame) reproduces the identical shape: after `session/prompt`,
goose sends one `session_info_update` (`activeRunId=run_…`) and then
**nothing**. Notes: goose answers `session/new` with
`currentModeId:"auto"` (mcremote later applies its safer `approve`
default per MADR 0069 D3 — not implicated; probe hung in `auto` too).

### 1.5 Goose's own logs name the cause

`~/.local/state/goose/logs/cli/2026-08-05/*.log`:

```text
WARN Provider request failed with status: 429 Too Many Requests.
     Payload: {"type":"error","error":{"type":"GoUsageLimitError",
     "message":"Weekly usage limit reached. Resets in 4 days. …
     https://opencode.ai/workspace/wrk_01…"}}
WARN Request failed, retrying (1/3): RateLimitExceeded …
INFO Backing off for 3600s before retry   ← target=goose_provider_types::retry
```

`~/.local/state/goose/logs/llm_request.<uuid>.jsonl` for the probe turn
contains the full request (model `minimax-m3`, provider `opencode_go`)
and **no response**. `~/.config/goose/config.yaml` line 95:
`active_provider: opencode_go`.

## 2. Root cause

| Layer | Behavior | Verdict |
| --- | --- | --- |
| goose provider `opencode_go` | 429 weekly-quota exhausted | trigger |
| goose retry policy | 1-hour silent backoff, ×3, no ACP error/update emitted while waiting | **root cause of the silence** |
| mcremote `runTurn` | cancel-proof wait on `session/prompt` (by design, sessions outlive disconnects) | correct, but blind |
| phone | renders nothing because nothing arrives | correct |

The stall watchdog (`turn_stall_notice_seconds: 120`) should emit a
notice at 120 s; observation windows here were 60–90 s, so its live
behavior on this path is **unverified** — and even if it fires, a
one-line "stalled" notice against a silent 3×3600 s retry loop is not an
acceptable UX for a quota error the agent knew about at t≈300 ms.

## 3. Findings — goose hang chain (verified ✅)

### F1 — Provider quota/backoff is invisible to the user (S0, product) ✅

Goose knows within ~300 ms that the turn cannot proceed for ~1 h, and
neither goose (no ACP notification) nor mcremote (no engine-log
scraping, no turn deadline) surfaces it. Remediation options, best
first:

- Tail the engine's structured logs (`lineRing` already captures stderr;
  goose writes JSON logs) and convert provider `RateLimitExceeded` /
  429 into a `TypeError` event with `agenterr` classification —
  `agenterr.Classify` already understands quota text and reset times.
- And/or: per-turn ceiling (config, default off or generous, e.g. 15 m)
  after which the turn emits `turn_complete/error` with a retryable
  classification, instead of waiting indefinitely.
- Upstream: file against goose — a 429 with a known reset time should be
  a `session/update` (or prompt error), not a silent 1 h sleep.

### F2 — No prompt-dispatch log in acphttp (S1, observability) ✅

`internal/provider/acphttp/session.go` logs nothing when a turn begins
or when `session/prompt` is written to the engine socket.
`opencode-http` logs `prompt_async … ok=true`; goose turns are invisible
in `mcremote.err.log`, which cost most of this incident's diagnosis
time. Add a log line in `beginTurn`/`runTurn` (session id, agent id,
model) and on turn end (stop reason, duration).

### F3 — `pair create` tokens are rejected by the already-running daemon (S1, verified symptom ✅, mechanism unconfirmed)

A token minted by CLI `mcremote pair create` at 18:19 was rejected
(HTTP 401) by the daemon that had been running since 18:10 — and the
401 produced **no log line at info level**. Likely the daemon holds the
device store in memory from startup (CLI wrote `devices.json` after
load); needs confirmation. Two sub-findings: (a) daemon should re-read
or watch the store (or the CLI should nudge it via the admin socket);
(b) auth rejections must log at warn with a reason. Related: §6 item 7
(store TOCTOU on creation).

## 4. Findings — transport / ws / relay (agent-ranked, spot-checked)

Reported by the transport auditor; F-T1's code path was read and
confirmed as described (marked ✅ where I re-read the code myself).

- **F-T1 (High) ✅** `internal/ws/server.go:269-276` —
  `BroadcastEvent` silently drops all events for a session whose owner
  `OwnerOf` can't resolve; no log, no counter. Any provider/session id
  mismatch or post-delete event becomes an undebuggable client-side
  hang. Also `Manager.Create` keys by `sess.ID()`, not the requested
  `LocalSessionID` (`internal/session/manager.go:439`).
- **F-T2 (High)** `internal/ws/server.go:787-803` +
  `internal/ws/idempotency.go:77-89` — a same-id retry of an in-flight
  or failed `session.create`/`session.prompt` can receive **no response
  frame at all** (ledger entry deleted on `fail()`, or waiter ctx
  expiry) — dead air on the client.
- **F-T3 (Med-High)** `internal/ws/server.go:673-720, 1268, 1286,
  1412-1455` — `session.set_mode`, `set_config_option`, `cancel`,
  `permission.respond`, `question.respond` run inline on the WS read
  loop with no deadline; a wedged engine freezes the entire connection
  (pings included).
- **F-T4 (Medium)** `internal/ws/idempotency.go:132-154` — error
  envelopes are `capture`d as finished results, so same-id retries
  replay stale `turn_busy`/timeout errors forever instead of
  re-executing.
- **F-T5 (Medium)** `internal/ws/server.go:141-145, 746-760` — async-op
  slots leak when a handler ignores ctx; 8 leaks = permanent
  `rate_limited` for the connection until reconnect.
- **F-T6 (Medium)** `internal/relay/server.go:634-654` +
  `internal/relay/hub.go:239-334` — claim/publish vs phone-timeout race
  leaks the tunnel (`<-pending.done` never closed) and burns phone
  slots (`MaxPhonesPerHost`) until sweeps recover, reading as "can't
  reach host" for up to ~60 s.
- **F-T7 (Med-Low)** `internal/relay/hub.go:135-145` — host-control
  dial write has no deadline and serializes all joins behind `writeMu`.
- **F-T8 (Low)** `internal/certs/acme.go:174-179` — letsencrypt TLS
  config advertises `h2` first; an h2-negotiating WS client cannot
  upgrade (selfsigned mode is safe). The relay serves only WS and
  should not offer h2 at all.
- **F-T9 (Low)** `internal/ws/server.go:558-563` — Bearer-header auth
  on upgrade sends no `auth_ok` and never negotiates v2.
- Verified clean: all 24 protocol client→server methods are dispatched;
  writer-per-conn with bounded queue; v2 liveness separation.

## 5. Findings — session/event layer (agent-ranked)

- **F-S1 (High)** `internal/session/manager.go:605-658` +
  `internal/session/commands.go:883-900` — seq is stamped under `m.mu`
  but broadcast after unlock: live delivery order ≠ seq order, and one
  inversion is deterministic at the start of **every new session**
  (advertiseCommands broadcasts seq N+1 before the triggering event's
  seq N). Any client "seq ≤ maxSeen ⇒ covered" fast path (cf.
  `a4029f1`) silently drops the lower-seq event.
- **F-S2 (High)** `internal/session/manager.go:653-658` — events with
  `Replay=true` are suppressed from live broadcast with no diagnostic;
  acphttp stamps `Replay` on everything emitted while `loading` — a
  stuck/mistimed `loading` flag silently converts a live session into
  history-only. The exact "history fills, nothing streams" shape.
- **F-S3 (High)** `internal/session/manager.go:770-800` — `Authorize`
  first-touch-claim race: two devices can both be told "authorized"
  while memory and disk owners diverge; the loser's events fan out to
  the other device only — reads as a hang on the losing phone.
- **F-S4 (Med-High)** `manager.go:1462-1469` + `461-471` — seq reuse
  within one epoch after a failed close-time `SaveHistory`; client
  dedupe then drops the recreated session's live events.
- **F-S5 (Medium)** `manager.go:1716-1737` vs `1403-1480` —
  `FlushHistory` vs `Close`/`Delete` race: stale snapshot can clobber
  the final transcript, or resurrect a purged session dir
  (`Store.List` then reports `Degraded=true` forever).
- **F-S6 (Medium)** `manager.go:227-247, 364-365` — `lockCreate` is not
  ctx-aware and is held across provider `Start`; one hung engine start
  wedges every later create/relaunch for that session id with no
  timeout.
- **F-S7 (Medium)** `commands.go:866-868` vs `manager.go:461-471` —
  `/clear` doesn't clear the durable ring; a cold reload replays the
  entire pre-clear conversation.
- **F-S8 (Low-Med)** pump does fsync'd multi-session history flushes
  before broadcasting (`manager.go:640-642, 1668-1675`) — periodic
  streaming stalls under load. Plus: notices for untracked sessions
  broadcast with `Seq=0` (`commands.go:883-899`); cleared pending asks
  emit no `permission_resolved` (`manager.go:625-627`, stale permission
  card = perceived hang); `agenterr` quota keywords over-broad
  (`agenterr.go:114-135`); `HistoryPage` O(n²) marshal
  (`manager.go:895-905`).

## 6. Findings — CLI / config / auth / update (agent-ranked)

1. **(High)** `internal/update/run.go:118-130` + `swap.go:69-133` —
   `HealStart` unconditionally true: `mcremote update` on a host
   without an installed service swaps, fails to start, rolls back, and
   loses the staged update.
2. **(High)** `internal/cli/service/setup.go:442` — macOS
   `setup-service --no-start` still `bootout`s a running service and
   never bootstraps it again.
3. **(High)** `internal/cli/service/defaults_mcremote.yaml` +
   `configs/config.example.yaml:85` — provisioned configs write
   `permission_mode: ""` for grok, silently undoing the MADR 0050 D3
   pinned `"default"`; template-parity test never compares against
   `Defaults()`.
4. **(Medium)** `internal/config/load.go:253-347` — missing
   `SetDefault`s: `limits.ws_read_deadline_seconds`,
   `limits.ws_resume_window_seconds`, `limits.tcp_keepalive.*`,
   `providers.codex.sandbox_broken_policy` env vars are silently
   ignored (MADR 0068/0048 knobs).
5. **(Medium)** `internal/cli/serve.go:50-55` + `paths.go:30-35` — raw
   `--data-dir` re-applied over Load's absolutized value; relative
   paths hard-error after Load accepted them.
6. **(Medium)** `internal/update/run.go:98-112` — update swaps the
   *running binary's* path, not the service's ExecStart binary; reports
   success while the daemon still runs the old build.
7. **(Medium)** `internal/auth/store.go:219-226` — `OpenStore`
   creates/truncates `devices.json` outside the flock (TOCTOU between
   CLI `pair create` and daemon startup — see §3 F3).
8. **(Medium)** `providers.goose.args` / `providers.goose.fs_roots`
   parse but are silently discarded (`config.go:492`,
   `daemon.go:522`) — no behavior, no warning (contrast the loud
   `providers.opencode.transport` retirement error).
9. **(Medium)** systemd user unit sets `PrivateTmp=true` while the
   appdirs runtime fallback is under `os.TempDir()`
   (`internal/appdirs/roots.go:96-102`) — admin-socket path can split
   between daemon and CLI namespaces, silently breaking device kicks.
10. **(Low)** `internal/picker/cache.go:98-116` inflight entry leaks on
    fetch panic; `internal/auth/paircode.go:189,197` unchecked burns;
    `internal/update/github.go:116-121` SHA256SUMS prefix match;
    `internal/cli/service/control.go:172-174` hard-coded unit label vs
    `--unit-name`; smoke-protocol still v1-only with a dead
    `if waitAfter > 0 {}` block (`scripts/smoke-protocol/main.go:405`).
11. Also noted: there is **no "claude" engine** anywhere in the Go
    side, and `mcremote doctor` has zero per-engine checks (binary
    presence, provider readiness) — a hung provider is invisible to it.

## 7. Findings — Flutter app (UI subset only; agent-ranked)

The event-ingestion layer (chat_models / transcripts_notifier / WS
mapping) was **not** audited (sweep stopped early to conserve quota);
the UI files audited cannot blank a transcript, consistent with the
server-side root cause.

1. **(High)** `TranscriptCache` serialization defeated by second
   instances (`settings_screen.dart:565,602` construct fresh caches
   whose `_serial` queues don't serialize against the live notifier's) —
   clear-while-streaming corrupts the index (MADR 0046 H-C class).
2. **(Medium)** `connect_screen.dart:133-137` — hand-editing Host
   clears pair hints without `setState`; stale "Claim & connect" UI
   then fails with "Host and token required" after a good QR scan.
3. **(Medium)** `connect_screen.dart:846-864` — `_busy` released
   between clipboard read and claim; double-tap can burn a one-shot
   pair code.
4. **(Medium)** `chat_bubble.dart:826-844` — post-frame streaming
   callback doesn't re-check `widget.streaming`; a finalized reply can
   be re-rendered as streaming and stick (blinking caret forever).
5. **(Low)** `settings_screen.dart:540` unguarded `clearSecrets()`
   (silent dead tap on keystore failure); `notification_service.dart`
   `init` not re-entrancy guarded; `app_update.dart:115-120` catch can
   mask the original error and leak a partial APK.

## 8. Root-cause map (symptom → fix)

| Symptom | Cause | Fix |
| --- | --- | --- |
| New goose session + prompt → silence | goose provider 429 + silent 3600 s backoff (§2) | Ops: switch goose model/provider now. Code: F1 (surface backoff), F2 (log dispatch) |
| Same, but provider healthy (latent) | F-T1 owner-drop, F-S2 Replay-suppress, F-S3 owner race, F-S1/F-S4 seq drops | §4/§5 triage, in that order |
| Phone retry gets dead air | F-T2 / F-T4 idempotency holes | rework capture/fail semantics |
| Whole connection freezes | F-T3 inline provider calls on read loop | per-op deadlines + async |
| Operator can't see any of it | F2, F3(b), F-T1's silent drop, doctor gaps (§6.11) | log lines + counters + doctor engine checks |

## 9. Decision

Record findings now; remediate in priority order (S0/S1 first: F1, F2,
F3, F-T1, F-T2, F-S1–S3, §6.1–3) in a follow-up plan
(`0073-PLAN-…`, not yet written). The goose quota itself is an operator
action, not a code change, and unblocks the phone today.
