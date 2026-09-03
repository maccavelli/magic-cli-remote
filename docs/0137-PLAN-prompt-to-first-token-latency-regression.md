---
status: in-progress
date: 2026-09-03
associated-madr: "0137-MADR-prompt-to-first-token-latency-regression.md"
---

# Implement: restore first-token latency — delivery, instrumentation, pins, and per-provider optimisation

Associated MADR: [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md)

## Goal

Make `hi` answer in about a second again on **all five providers** (grok, kilo,
opencode, goose, codex), and make the next regression detectable by the daemon
rather than by the operator. In order: measure the turn, pin every provider to
the version actually installed, then remove the avoidable per-provider cost —
including the Codex deprecation notice that fires on every session start.

The providers do not share a transport, so "for all providers" is stated as a
matrix below and each phase says what it means for each one, rather than
assuming a fix in one package reaches the others.

## Scope

**Provider coverage.** Every phase applies to all five providers. Because they
do not share a transport, each phase states what it means per provider:

| provider | transport | package | pin | prewarm | available_commands |
| --- | --- | --- | --- | --- | --- |
| grok | ACP stdio | `acpagent` | new | **yes (F5)** | yes |
| kilo | HTTP+SSE | `httpagent` | update | no | yes |
| opencode | HTTP+SSE | `httpagent` | update | no | yes |
| goose | ACP/HTTP | `acphttp` | new | no | yes |
| codex | app-server | `codex` | new | no | no |

**In scope:**

* `internal/provider/httpagent/` — the SSE pump lifecycle and the
  agent-session → local-session binding (the kilo 7.5.6 delivery failure).
* `internal/provider/kilo/version.go`, `internal/provider/opencode/version.go`
  — pins moved to installed versions.
* New version gates for `grok`, `goose`, `codex`, following the existing
  `KnownGoodVersion` shape.
* `internal/provider/acpagent/acpagent.go` — prewarm re-arm scheduling (F5).
* `internal/provider/acpagent/session.go:1471` — `available_commands` dedupe
  (F2).
* `internal/provider/codex/store_reality.go` — probe cost (F1).
* `internal/ws/server.go` — per-turn latency + token instrumentation; the
  remaining inline handlers (F4).
* `internal/session/` and `internal/protocol/` only as far as carrying the new
  latency/version signals to the phone.
* `internal/wirecap/` (new) — the env-gated in-daemon frame capture, plus its
  four transport hooks in `httpagent`, `acpagent`, `acphttp` and `codex`.
* Tests for each of the above, plus fixtures captured from the installed
  provider versions.

**Explicitly out of scope:**

* **Changing which model a provider defaults to, or pinning models anywhere.**
  Per the MADR's accepted constraint, a provider with an established default
  model runs on it, and a model is chosen only when the user asks for one. This
  plan measures the default's cost; it never substitutes it. `model: ""` stays
  the correct posture in `config.yaml`.
* Removing the operator's MCP servers or kilo plugins. Prompt weight is
  attributed in Phase 6 and acted on only after.
* The phone app. No Flutter change is required by any phase here.
* MADR 0135 (manifest versioning) and the outstanding 0136 host verification.
* Upgrading or downgrading any provider binary. Pins move to what is
  installed; the binaries are not touched.

## Implementation Steps

Every step names the file it touches and the check that proves it. A step whose
outcome would be a judgement call has that call made here, not deferred to
execution.

### Phase 0 — withdrawn

**Withdrawn 2026-09-03; nothing implemented.** It was built on a reproduction
that was my harness reusing request id `c1`, which the daemon correctly
collapsed via idempotent replay (`internal/ws/server.go:925-928`, MADR 0095
D6/F5). See the MADR's "Correction, 2026-09-03". Verified with
`scripts/smoke-protocol`: a kilo 7.5.6 turn completes end to end.

No daemon change follows. The one durable obligation is procedural and lands in
Phase 1 step 1.4. The unreproduced indefinite hang stays open, watched in
Phase 5 step 5.4.

### Phase 1 — a wire-capture tool, and fixtures per installed version

**Files:** `scripts/capture-wire/main.go` (new);
`internal/provider/<provider>/testdata/wire/<version>/` (new).

1.1 Two capture mechanisms, because the five transports do not share a seam:

  *(a)* `scripts/capture-wire/` for the SSE providers (kilo, opencode) — an
  external HTTP GET, no daemon required.

  *(b)* `internal/wirecap/`, an **in-daemon** capture gated by
  `MCREMOTE_WIRE_CAPTURE_DIR`, for the three the external tool cannot reach:
  grok (ACP stdio, SDK owns the read loop — hooked by teeing the reader), goose
  (ACP websocket), codex (JSON-RPC app-server). `wirecap.For` returns nil when
  the variable is unset and every method is nil-safe, so production pays one nil
  check per frame. Redaction runs at capture time, in both the absolute and
  separator-stripped forms of the home path.

1.2 Capture one `hi` turn each for kilo **7.5.6** (SSE), opencode **1.18.26**
(SSE), goose **1.48.0** (ACP/HTTP) and codex **0.152.1** (app-server
JSON-RPC). The codex capture must include the session-start notifications, so
the `deprecationNotice` of F6 is in the fixture rather than described from
memory.

1.3 **grok 1.0.13: capture the lifecycle without spending quota.** A session
create with no prompt yields `initialize`, session lifecycle and the `_x.ai/*`
extension frames at zero model cost. The message-streaming shapes need a prompt
and therefore the owner's approval; until then grok's fixture carries a `note`
saying exactly what it does and does not contain, so the gap is visible rather
than implied.

1.4 Prove the tool can fail: run it against a port with no engine, confirm a
non-zero exit and an explicit error, and record that output in the execution
record. No fixture is trusted until this is done.

**Verification:** `go build ./scripts/capture-wire`; every `frames.jsonl` is
non-empty and its `meta.json` version equals `<bin> --version`.

**Done when:** four fixture sets exist (five with grok), each version-stamped,
and 1.4's failure output is recorded.

### Phase 2 — per-turn latency and token instrumentation

**Files:** `internal/session/manager.go`, `internal/session/manager_test.go`.

2.1 Record three timestamps per turn on the session entry in
`internal/session/manager.go`: `promptAt` in `Manager.Prompt` (line 1634);
`firstOutputAt` in the event pump on the first `assistant_message_chunk`,
`thought_chunk` or `tool_call` after `promptAt`; `turnEndAt` in the pump's
existing `event.TypeTurnComplete` / `event.TypeError` case (line 1012).

2.2 On turn end emit exactly one info log, `msg="turn latency"`, fields:
`session_id`, `provider`, `model`, `ttft_ms`, `turn_ms`, and when the provider
reports them `input_tokens`, `output_tokens`, `cache_read`, `cache_write`. One
line per turn; no per-chunk logging.

2.3 The record states cache warmth: `cold=true` when `cache_read == 0`, else
`cold=false`. Cold and warm are two populations; a number that does not say
which is not evidence.

2.4 **No protocol change in this phase.** Surfacing latency to the phone is a
separate decision, deliberately not bundled with instrumentation.

2.5 Tests: a fake-provider turn emits exactly one `turn latency` record;
`ttft_ms` measures to first output, not to `turn_complete`; a turn producing no
output emits a record with `ttft_ms` absent rather than zero.

**Verification:** `go test ./internal/session/... -count=1`; a live `hi` on the
isolated daemon yields one `turn latency` line whose `ttft_ms` is within 200 ms
of the value computed from that session's `history.json`.

**Done when:** the MADR's latency table is reproducible from these log lines
without parsing `history.json`, and 2.5 fails against pre-change code.

### Phase 3 — pins at the installed versions, all five providers

**Files:** `internal/provider/kilo/version.go`,
`internal/provider/opencode/version.go`; new `version.go` under
`internal/provider/{grok,goose,codex}/` and their engine-ready call sites.

3.1 `kilo` `KnownGoodVersion` -> `"7.5.6"`; `opencode` -> `"1.18.26"`.

3.2 Add `version.go` for `grok` (`"1.0.13"`), `goose` (`"1.48.0"`), `codex`
(`"0.152.1"`), copying the kilo/opencode shape: the constant, a
`VersionIsKnownGood` helper, and a warn-on-mismatch call from the engine-ready
path.

**Amended 2026-09-03:** the version each of those three compares against comes
from the source step 7.5 (as amended) reads for it — `agentInfo.version` for
goose, `_meta.agentVersion` for grok, the `userAgent` prefix for codex. 7.5
therefore lands with, not after, this phase.

3.3 **Mismatch warns; it never refuses to start.** A routine upstream upgrade
must not become an outage. State that in a comment above each constant so a
later reader does not harden it into a refusal.

3.4 Each constant cites its Phase 1 fixture directory as the evidence the wire
shape was re-checked — or, for grok if 1.3 was declined, says no fixture exists.

3.5 **No phone-facing change.** Reporting drift to the phone needs a protocol
field and a UI affordance; neither is justified before Phase 5 shows whether
drift is implicated. Dropped from this plan.

**Verification:** `go test ./internal/provider/... -count=1`; the isolated
daemon starts with no `differs from known-good pin` warning for any provider.

**Done when:** all five constants equal the installed versions and startup is
clean.

### Phase 4 — optimisations, stated per provider

**Files:** `internal/provider/acpagent/{acpagent.go,session.go}`,
`internal/provider/acphttp/session.go`, `internal/provider/kilo/command.go`,
`internal/provider/opencode/command.go`,
`internal/provider/codex/{store_reality.go,logout.go}`,
`internal/ws/server.go`, plus tests alongside each.

4.1 **F5 — prewarm re-arm off session create. grok only.** `EnsureWarm` exists
only in `acpagent`; `httpagent`, `acphttp` and `codex` keep a shared engine and
need no change, which is stated here so a later reader does not go looking.
Delete `defer p.EnsureWarm()` from `Start` (`acpagent.go:775`) and call
`p.EnsureWarm()` from the turn-end path in `session.go` (lines 413-425, where
`TypeTurnComplete` and `status:"idle"` are emitted), so the ~3.8 s replacement
spawn happens after the first turn rather than during it.
*Test:* `EnsureWarm` is not called between `Start` returning and the first
`turn_complete`, and is called exactly once after it.

4.2 **F2 — `available_commands` dedupe. Four providers.** Emitted by
`acpagent` (grok), `acphttp` (goose), `kilo` and `opencode`. Only grok was
observed spamming — its source re-sends even when the list is unchanged — but
the dedupe goes where all four inherit it: add a comparison helper in
`internal/event` and use it at each of the four emit sites, skipping the emit
when the new list equals the last one sent for that session (same names,
descriptions and hints, in order).
*Test, per provider:* ten identical updates produce one event; changing one
description produces a second.

4.3 **F6 — Codex deprecation notice. Two parts, two owners.**
  *(a) mcremote:* dedupe `notice` events the same way as 4.2 — suppress a
  notice whose kind and text equal the last one emitted for that session. One
  codex session recorded 77 notices; a once-per-session upstream notice must
  not become per-session noise. ~~Applies at every `TypeNotice` emit site.~~

  **Amended 2026-09-03.** There are **42** `TypeNotice` emit sites across six
  packages. The dedupe goes in the session event pump
  (`internal/session/manager.go`) instead, where every provider's events
  converge and the per-session `entry` state already lives — one guard covering
  all 42, and any future emit site, rather than 42 that a new site silently
  opts out of. A replayed event is never suppressed. `internal/session/manager.go`
  is added to this phase's file list.
  *(b) operator, not code:* the notice itself is caused by
  `[features] codex_hooks = true` in `~/.codex/config.toml`. `codex_hooks` is a
  legacy alias for canonical key `hooks`, which is `Stable` and
  `default_enabled: true`, so **the block is redundant and can simply be
  deleted**. Report this to the owner in Phase 5 and let them decide; mcremote
  must not edit the user's codex config.
  *Test:* ten identical notices produce one event; a differing notice produces
  a second.

4.4 **F1 — the Codex probe leaves the phone-triggered path. codex only.** Add
`ObserveCredentialStoreCachedNonBlocking` to
`internal/provider/codex/store_reality.go`: return the cached value when fresh,
else return `RealityUnknown` immediately and start at most one background
refresh. `backupProjection` (`codex/logout.go:241`) calls it. Synchronous
probing is unchanged on the recovery and `CheckBackend` paths, which are not
phone-triggered. *Chosen over* narrowing `doctor --json` or widening the cache
window: both leave a ~1.4 s network-dependent call reachable from
`providers.list`, and this removes it outright without weakening MADR 0136's
classification.
*Test:* `AuthStatus` with a cold cache invokes no binary and returns the
unknown/unsupported projection rather than a fabricated one.

4.5 **F4 — inline ws handlers, decided per handler. All providers.** In
`internal/ws/server.go` move `session.list`, `session.set_mode` and
`session.set_config` to `dispatchAsync`; they can touch provider state.
**Keep** `session.cancel`, `session.pending_asks` and `oauth.cancel` inline,
with a comment stating why: they are control-plane operations that must not
queue behind `maxAsyncPerClient = 8`, and a cancel that waits for a slot
defeats its own purpose.
*Test:* a 2 s block in `session.set_config` does not delay a following
`session.prompt` on the same connection.

**Verification:** `go test ./internal/provider/... ./internal/ws/...
./internal/event/... -count=1`; each of 4.1-4.5 has a test that fails against
pre-change code, output recorded.

**Done when:** all five land with fail-first evidence, and 4.2/4.3 are verified
on every provider that emits the event rather than only on grok.

### Phase 5 — verify on the reporting host

**Files:** none; verification only.

5.1 `make install`, restart, then three `hi` turns on **each of the five
providers** from the phone — not only the two that produced the original
report.

5.2 Read `ttft_ms` and `cold` from the Phase 2 records. Report cold and warm
separately; never average across them.

5.3 Compare against the MADR: historical 0.6-2.9 s, regressed 5-18.6 s, warm
direct-engine 0.79-0.97 s, cold ~14 k-token prefill. State plainly whether the
gap closed, partially closed, or did not. If warm turns are fast and cold ones
are not, say exactly that rather than claiming a fix.

5.4 Confirm codex starts a session with **no deprecation notice repeated**, and
report F6(b) to the owner: `[features] codex_hooks = true` in
`~/.codex/config.toml` is a redundant legacy alias for `hooks`, which is
enabled by default, so the block can be deleted. Do not edit that file.

5.5 Watch for the unreproduced hang. If a turn yields neither output nor error,
capture the `turn latency` record, the session `history.json`, and the engine's
own view of that session **before** restarting anything.

**Done when:** three cold and three warm measurements exist for **each of the
five providers** in the execution record, with the verdict stated honestly, and
F6(b) has been reported to the owner.

### Phase 6 — attribute the prompt weight (investigation, no code)

**Files:** none. The output is a written finding in the execution record.

6.1 **Establish why `cache.write` is 0 in production** while the isolated engine
shows `cache_read: 14336`. Compare the two engines' configuration and the
request bodies mcremote sends. First, because a working cache removes most of
the latency and the cost without removing capability and without touching the
model.

6.2 Then, **and only with the owner's approval to spend quota**, run the
prompt-weight arms: with and without the `magictools` MCP server, with and
without the two kilo plugins. Each arm records `input_tokens`, `cache_read`,
`ttft_ms` and `cold`, cold and warm kept separate.

6.3 Report the numbers and stop. **Do not** remove an MCP server or plugin, and
**do not** pin or swap a model — providers run on their own default model
unless the user specifies otherwise. Phase 6 produces a recommendation, not a
change.

**Done when:** the finding is written with per-arm numbers and any proposed
change is left for the owner to approve.

### Phase 7 — consume the provider surface the current versions offer

Added 2026-09-03 after driving all five engines (MADR fifth amendment). Pinning
records the version; this phase is what "optimised for the current versions"
means. Each item is independent and may land separately.

**Files:** `internal/provider/kilo/dialect.go`,
`internal/provider/httpagent/provider.go`,
`internal/provider/opencode/http.go`, `internal/provider/codex/routing.go`,
`internal/provider/acpagent/acpagent.go`,
`internal/provider/acphttp/{conn.go,session.go}`.

7.1 **F7 — consume kilo's `sync` stream (highest value).** 18 of 56 fixture
frames are `sync`, carrying `seq` per `aggregateID`. Decode it, track the last
`seq` per session, and on SSE reconnect resume from it instead of relying on
`resyncSessions` polling. Do **not** double-deliver: `sync` duplicates the
ad-hoc frames, so a consumed `sync` event must be de-duplicated against the
plain frame by event id.
*Test:* a fixture replay with a simulated stream drop delivers every event
exactly once, and the post-drop gap is filled from `seq` rather than a poll.

7.2 **F8 — handle opencode `catalog.updated`.** Invalidate the cached model
catalog (`p.catalogs`) when it arrives, so the phone stops being offered a
stale model list.
*Test:* a `catalog.updated` frame invalidates the cache; the next
`ListModels` re-harvests.

7.3 **F8b — `plugin.added` noise.** 45 frames in one short turn. Fold into the
4.2/4.3 dedupe rather than adding a third bespoke suppressor.

7.4 **F9 — route codex's `modelProvider/authRecoveryStarted|Completed`.**
Surface them as provider conditions. MADRs 0133/0134/0136 inferred credential
state from files and probes; codex now reports it. Leave the other six
unrouted notifications alone — they are recorded, not adopted, because
`rawResponse*` and `thread/realtime/*` serve features mcremote does not have.
*Test:* each notification produces exactly one provider event, and an unknown
notification is still ignored rather than forwarded.

7.5 ~~**F10a — read `agentInfo.version` from the ACP handshake** and use it as
the version source for the grok and goose pins added in Phase 3, replacing
whatever ad-hoc source those pins would otherwise need.~~

**Amended 2026-09-03 (see the deviation below and the MADR's ninth
amendment).** `agentInfo` is optional in ACP and grok omits it. Read each
engine's own source instead: `agentInfo.version` for goose
(`acphttp`), `_meta.agentVersion` for grok (`acpagent`), and the `userAgent`
prefix for codex (`codex/provider.go`, cross-checked against `cliVersion` on
`thread/started`). Each reader fails to empty, never to a wrong value, and an
unreported version produces no warning.

7.6 **F10b — honour `promptCapabilities` in `acphttp`.** It never reads them,
so goose's `image: true` is unobserved and images cannot be sent to a provider
that accepts them. Read the capability and gate image attachments on it, as
`acpagent` already does for grok (where it is correctly `false`).
*Test:* a stub advertising `image: true` accepts an image attachment; one
advertising `false` rejects it before sending.

7.7 **Recorded, not adopted:** grok's `sessionCapabilities`
(`list`/`resume`/`close`), `embeddedContext`, `x.ai/fs_notify` and
`x.ai/hooks` are unconsumed. `x.ai/hooks` in particular is a blocking
permission mechanism that overlaps mcremote's own auto-approve; adopting it is
a design decision needing its own record, not a gap to close here. This step
produces no code — it exists so the next reader knows the omission is deliberate.

7.8 **F11 — grok `_x.ai/*` extensions.** Measured live: grok emits ten, and
mcremote handles three, leaving **52 of 247 frames (21%) of one turn**
unhandled. Route the three that carry signal — `_x.ai/mcp/init_progress` and
`_x.ai/mcp/servers_updated` (MCP startup progress and membership, which bear
directly on first-turn latency) and `_x.ai/session/prompt_complete` (an explicit
turn-completion signal mcremote currently infers). Assess
`_x.ai/announcements/update`, `_x.ai/settings/update`, `_x.ai/sessions/changed`
and `_x.ai/queue/changed`, and either route or explicitly decline each.
*Test:* each routed extension produces one provider event; an unknown `_x.ai/*`
notification is ignored rather than forwarded.

7.9 **Added 2026-09-03 — capture the `acphttp` handshake.** `initialize`,
`authenticate` and every other `postJSON` call goes over `POST /acp`, not the
websocket, so the goose fixture begins at the `session/new` result and contains
no handshake at all. Hook `postJSON` (`internal/provider/acphttp/conn.go:62`)
under the existing `MCREMOTE_WIRE_CAPTURE_DIR` gate and the existing redaction,
then re-record the goose fixture with the handshake included.
*Test:* a capture run records the initialize request and response; redaction is
verified at zero occurrences on the re-recorded fixture.

**Done when:** 7.1-7.6, 7.8 and 7.9 land with tests, and 7.7 is confirmed as a
written decision rather than an oversight.

## Verification

Run after every phase:

```bash
make pre-add-check
go test ./internal/... ./cmd/... -count=1
make vet
make lint
```

Host checks after Phase 5:

```bash
grep -a "turn latency" ~/Library/Logs/mcremote/mcremote.err.log | tail -20
grep -a "differs from known-good pin" ~/Library/Logs/mcremote/mcremote.err.log | tail -5
```

### Acceptance criteria

* Every phase's new tests fail against pre-change code, with the failure output
  recorded. A check never seen to fail is not evidence.
* Every latency number recorded anywhere states `cold` or `warm`. Conflating
  them produced two wrong conclusions in this record already.
* Any diagnostic harness written for this work uses a unique request id per
  request and is demonstrated to fail before its output is trusted.
* Exactly one `turn latency` line per turn, carrying `ttft_ms`, `turn_ms` and
  `cold`; `ttft_ms` within 200 ms of the value computed from that session's
  `history.json`.
* All five providers have `KnownGoodVersion` equal to the installed version,
  each citing its fixture or explicitly stating none exists.
* `EnsureWarm` is proven by test to run after the first turn, never during
  session create.
* Ten identical `available_commands_update` frames yield one event, verified
  on each of the four providers that emit them (grok, goose, kilo, opencode).
* Ten identical `notice` events yield one event; a differing notice yields a
  second.
* A codex session start reports its deprecation notice at most once.
* Latency measurements exist for all five providers, not only kilo and grok.
* `AuthStatus` on a cold reality cache invokes no binary.
* A 2 s block in `session.set_config` does not delay a following
  `session.prompt` on the same connection.
* Phase 6 reports numbers and recommends; it changes no model, no MCP server,
  no plugin.
* `git diff --stat` touches only files named in Scope.

## Rollout and Rollback

**Rollout.** Daemon-side only; effective on the next restart. No phone update
is required by any phase. Phases 0-4 are independently committable and each is
independently revertable.

**Rollback.** Revert the phase commits. The pins are constants and the
instrumentation is additive, so a revert restores prior behaviour exactly.

**Sequencing.** Phase 0 is withdrawn. Phase 3 depends on Phase 1 (a pin cites
its fixture) and on 7.5 for the grok/goose version source. Phase 5 depends on
Phase 2 (it reads the `turn latency` record). Phase 6 depends on Phase 2 and on
Phase 5's host numbers. Phases 4 and 7 are independent and may land at any
point, except that 7.3 folds into 4.2/4.3 and should follow them. Order:
1 → 2 → 7.5 → 3 → 5 → 6, with 4 and the rest of 7 inserted where convenient.

**What this plan will not do.** It will not remove the operator's MCP server or
plugins, will not change or pin a provider's default model (an accepted
constraint of the MADR, not merely a scoping choice), and will not upgrade or
downgrade a provider binary. Where those turn out to be the dominant cost,
Phase 6 reports the number and the decision stays with the owner.

## Execution Record

### Operator config fix — 2026-09-03 (F6b), at the owner's request

`~/.codex/config.toml` had `[features] codex_hooks = true`. Backed up to
`config.toml.bak-20260903-122714`, then the two-line block was **removed** —
not renamed to `hooks = true`, because `hooks` is `Stable` with
`default_enabled: true`, so the setting was redundant rather than merely
deprecated.

Verified: `codex features list` reports `hooks  stable  true`, so the feature is
still on, and no legacy usage is attributed to the config. `codex doctor` also
now reports `auth: ok | auth is configured` — the owner's earlier re-login took,
independently of this change.

### Phase 1 — 2026-09-03, partially complete

**1.1** `scripts/capture-wire/` added: records an engine's raw `data:` payloads
to `internal/provider/<p>/testdata/wire/<version>/frames.jsonl` plus a
`meta.json` naming provider, version, source and capture time.

**1.4 The tool was proven able to fail before any fixture was trusted, and that
test found two real defects — one in the tool, one in my measurement:**

* Reading `$?` after a pipe returned `tail`'s status, so the first run reported
  `exit=0` for three genuine failures — the same class of measurement error that
  produced the retracted delivery-bug claim. Re-run without the pipe:

  ```text
  no-engine:    exit=1  capture-wire: Get "http://127.0.0.1:59999/global/event": … connection refused
  missing-flag: exit=1  capture-wire: -version is required
  bad-kind:     exit=1  capture-wire: unsupported -kind "stdio" (only sse today; …)
  ```

* **The tool wrote an empty `frames.jsonl` on failure**, because it created the
  file before capturing — exactly the "silently empty fixture" it exists to
  prevent. Fixed to capture into memory and write only on success; re-verified
  that a failed run now leaves no fixture at all.

**1.2 Fixtures captured** for the two providers whose pins need updating:

| fixture | frames | streaming shapes present |
| --- | --- | --- |
| `kilo/testdata/wire/7.5.6/` | 56 | `message.part.delta` x8, `message.part.updated` x7, `message.updated` x6, `sync` x18 |
| `opencode/testdata/wire/1.18.26/` | 85 | `message.part.delta` x8, `message.part.updated` x7, `message.updated` x6, `plugin.added` x45 |

**Redaction, added after the first capture leaked the operator's username.**
Fixtures go to a public repository and carried `/Users/<user>` in 69 and 8
frames. Redaction now happens at capture time, so a fixture cannot be created
unredacted and forgotten. A first attempt was **still not clean**: both engines
also emit the path with the leading separator stripped
(`"path":"Users/<user>"`), which replacing the absolute path alone misses —
caught by re-grepping a fixture that had already been "redacted". The tool now
substitutes both forms and records it in `meta.json`. Final state: zero
occurrences in either fixture.

**Verification.**

```text
go build ./...           -> ok
go vet ./scripts/...     -> clean
make pre-add-check       -> 717 file(s) clean (gofmt, golint, govulncheck)
frames.jsonl JSON parse  -> 0 malformed lines in both fixtures
```

### Deviations

**2026-09-03 — Phase 1 covers two providers, not four; `-kind stdio` is not
implemented.** Step 1.2 named kilo, opencode, goose and codex; only kilo and
opencode are captured.

*Reason.* The five do not share a transport. kilo and opencode are SSE and
capture externally with a plain HTTP GET. goose is ACP over HTTP; grok and codex
are stdio (ACP, and `app-server proxy`). Capturing those from outside requires
the tool to speak each full client handshake, duplicating mcremote's own client
logic in a script.

*Not worked around.* The tool rejects `-kind stdio` with an explicit error
rather than pretending, and no fixture exists for the three uncovered providers.

*Resolution needed before Phase 3 can cite fixtures for grok, goose and codex.*
The cheapest sound option is a capture hook inside the daemon, gated by an
environment variable, dumping each raw frame as the dialect decodes it — one
mechanism covering all five transports. That touches production code paths and
is a scope change this plan does not authorise. **Raised for the owner's
decision rather than taken.**

*Consequence of doing nothing:* Phase 3 can still pin all five, but the grok,
goose and codex pins would cite no fixture — precisely the "a pin bump with no
fixture is a claim that nothing changed" posture this phase exists to end, and
which kilo 7.5.6 already falsified once.

**2026-09-03 — grok fixture not attempted.** Step 1.3 gates it on the owner
confirming quota. Not yet asked, because the stdio gap above blocks it
regardless.

**Observed, not acted on:** opencode 1.18.26 emitted **45 `plugin.added` frames**
in one short turn — a third instance of the unconditional-emit pattern behind
F2 (`available_commands`) and F6a (`notice`). Recorded for Phase 4 to consider;
no change made.

**Corrected 2026-09-03:** an earlier note here claimed opencode did not warn
and that its gate is a floor. Both were wrong. `KnownGoodVersion` is an exact
pin (`CompareVersions == 0`), the same policy as kilo; opencode additionally
has a separate `MinVersion` hard floor for session-tree. And it *did* warn —
`opencode engine differs from the known-good release` — which I missed by
grepping kilo's wording against opencode's. See the MADR's fifth amendment.

### Phase 1 (continued) — 2026-09-03, complete

**Capture gap resolved in the daemon**, at the owner's direction.
`internal/wirecap` records raw frames where they arrive, one hook per
transport: the SSE line in `httpagent.streamOnce`; `conn.readPump` after
`transport.Read` for codex; `readPump` after `ws.Read` for goose; and a tee on
the `stdout` reader for grok, whose ACP SDK owns its own read loop. Gated by
`MCREMOTE_WIRE_CAPTURE_DIR`; `wirecap.For` returns nil when unset and every
method is nil-safe.

**Fixtures now exist for all five providers:**

| provider | version | frames | contents |
| --- | --- | --- | --- |
| kilo | 7.5.6 | 56 | full turn (external tool) |
| opencode | 1.18.26 | 85 | full turn (external tool) |
| codex | 0.152.1 | 38 | full turn (in-daemon) |
| goose | 1.48.0 | 15 | full turn (in-daemon) |
| grok | 1.0.13 | 19 | **session lifecycle only** — no prompt, so no quota spent and no message-streaming frames |

Redaction verified at zero occurrences of the operator's username in all five.

**F11 found by the grok capture:** grok emits five `_x.ai/*` extension
notifications and mcremote handles one. `_x.ai/mcp/init_progress` and
`_x.ai/mcp/servers_updated` report MCP startup progress and membership — the
"why is the first turn slow" signal this record has been chasing indirectly.
Added to Phase 7 as 7.8.

**Codex routing confirmed sound:** all twelve methods emitted during a real turn
are routed. The eight unrouted notifications are unexercised features, not
broken basics.

**Verification.**

```text
go build ./...                          -> ok
go vet ./internal/...                   -> clean
go test ./internal/... ./cmd/... -c=1   -> 42 packages ok, 0 FAIL
make pre-add-check                      -> 718 file(s) clean
```

### Deviations (continued)

**2026-09-03 — the in-daemon capture is a production-code change the plan did
not originally authorise.** Raised to the owner as the recommended resolution
and approved before implementation. Recorded in the MADR's sixth amendment as a
decision rather than absorbed silently. The cost is one nil check per frame on
each transport's receive path when the environment variable is unset.

**2026-09-03 — grok's fixture is lifecycle-only.** The standing constraint is
that no provider quota is spent on a test without asking, and the owner's
approval covered resolving the capture gap, not spending grok quota. The
zero-cost half was captured; the message-streaming half is still outstanding and
the fixture's `note` field says so. Phase 3's grok pin must cite that limitation
rather than implying full coverage.

### Phase 1 (grok live) — 2026-09-03, complete

The owner authorised grok quota. Three live `hi` turns through mcremote against
grok 1.0.13, in-daemon capture on.

**The standing "grok quota is exhausted" note is stale.** It dated from
2026-09-01 and had gated every grok test in this record. All three turns
completed and returned real replies.

**Latency, all cold (a fresh session each):** 5.26s, 2.57s, 14.36s. A 5.6x
spread across three identical prompts minutes apart on one binary, model and
host — upstream variance, not transport, and the reason Phase 2 must record
every turn rather than sample.

**grok fixture replaced** with the full turn: 247 frames covering lifecycle,
prompt, streaming and completion, redaction verified at zero occurrences. All
five fixtures are now full-turn except goose and codex, which are single `hi`
turns, and none is lifecycle-only.

**F11 corrected.** The earlier count ("one of five handled") came from a
lifecycle-only capture and a substring grep, and was wrong in both directions.
Verified by exact literal match: grok emits **ten** `_x.ai/*` methods, mcremote
handles **three**, and the seven unhandled account for 52 of 247 frames.
`_x.ai/session/prompt_complete` — an explicit turn-completion signal — was not
visible at all until a real prompt ran.

**F2 quantified inside a single turn:** `available_commands_update` fired **22
times** in one `hi`. The defect was previously inferred from a 301-event
session; this measures it per turn.

**Also observed, and not fixable here:** the model emitted **93
`agent_thought_chunk` frames** to answer `hi`. That is where the seconds go, and
no mcremote change shortens it — it belongs to Phase 6's attribution, and under
the accepted constraint the model stays as the provider default regardless.

### Phase 2 — 2026-09-03, complete

**2.1** `entry` carries `promptAt` and `firstOutputAt`. `promptAt` is set by
`markPromptStart` immediately before dispatch in **both** prompt paths —
`Manager.Prompt` and `submitUserPrompt` (`internal/session/commands.go:849`,
the canonical-command path such as `/plan <text>`), which the plan named only
one of. A dispatch that returns an error clears the clock, so a prompt the
engine never accepted times nothing. `firstOutputAt` is set in the pump on the
first `assistant_message_chunk`, `thought_chunk` or `tool_call`.

No `turnEndAt` field was added. The plan named one; the turn-ending event's own
timestamp is already in hand at that point, so a third field would have been a
copy of a local variable. The record is built from it directly.

**2.2-2.3** `internal/session/turnlatency.go` (new) builds and logs the record
outside `m.mu`, so a log write never happens under the manager lock.

**2.5 Tests fail against pre-change code — three deliberate breakages, each
verified to have landed, each run against a `cp` backup and restored
byte-identical:**

```text
A. pump hook removed (the pre-change tree)
   FAIL TestTurnEmitsExactlyOneLatencyRecord              wanted 1 record, got 0
   FAIL TestTTFTIsMeasuredToFirstOutputNotToCompletion    wanted 1 record, got 0
   FAIL TestTurnWithNoOutputOmitsTTFT                     wanted 1 record, got 0

B. ttft measured to turn end (r.ttft = end.Sub(promptAt))
   FAIL TestTTFTIsMeasuredToFirstOutputNotToCompletion
        ttft_ms = 1000, want ~200 (the first thought chunk)
        turn_ms (1000) and ttft_ms (1000) are the same measurement

C. ttft_ms always emitted, zero when absent
   FAIL TestTurnWithNoOutputOmitsTTFT
        ttft_ms = 0 on a turn that produced no output
```

Breakage A is the one that matters: it is the tree as it stood this morning, and
it produces no record at all.

**Verification.**

```text
go test ./internal/session/... -count=1   -> ok
go test ./internal/... -count=1           -> ok (whole tree)
make pre-add-check                        -> 719 file(s) clean
```

Live, one `hi` through the isolated daemon against kilo 7.5.6:

```json
{"msg":"turn latency","session_id":"450c2b5d-4d08-4569-94de-508ffe6d4575",
 "provider":"kilo","turn_ms":13670,"ttft_ms":5167,"cold":true,
 "context_used":14670}
```

Computed independently from that session's `history.json`: ttft **5167.103 ms**,
turn **13670.725 ms**. Delta **0 ms** against the plan's 200 ms criterion. The
MADR's latency table is now reproducible from the log alone.

### Deviations

**2026-09-03 — `model` is absent from the record on every default-model
session.** Step 2.2 lists `model` among the fields.

*Evidence.* The manager's only model is `Meta.Model`
(`internal/session/manager.go:773`), set from `StartOptions.Model` — the
client's request. This plan's own accepted constraint is that providers run on
their default model, so that field is empty in the normal case. The live run
above confirms it: the daemon logged `kilo default model resolved
model=kilo/kilo-auto/balanced` at engine-ready, and the turn record carried no
`model`. The resolved value lives in `internal/provider/kilo/dialect.go:136-143`
and reaches nothing outside that dialect. Surveyed across all five: only kilo
and opencode track a resolved default (`defaultModelID`) or log one.

*Not worked around.* The field was not filled with `StartOptions.Model`,
"default", or the provider id.

*Resolution, chosen by the owner:* add an optional
`provider.ModelReporter { Session; CurrentModel() string }`, read it at turn
end, implement it for kilo and opencode. No protocol change, so step 2.4 holds.
See the MADR's eighth amendment.

*Files added to this phase's scope beyond the plan's
`manager.go` / `manager_test.go`:* `internal/session/turnlatency.go`,
`internal/session/turnlatency_test.go`, `internal/session/commands.go`,
`internal/provider/provider.go`, `internal/provider/kilo/session.go`,
`internal/provider/opencode/http.go`.

*Consequence had it been deferred:* a future latency shift could not be
attributed to a model change from the record alone, which is half of what
Phase 6's attribution needs.

**Observed, not acted on:** kilo's usage report populated only
`context_used` and `cache_read`; `input_tokens`, `output_tokens` and
`reasoning_tokens` were absent. Step 2.2 makes those conditional, so the record
is correct — but Phase 6's attribution cannot rely on kilo
reporting them.

### Phase 2 (deviation resolved) — 2026-09-03, complete

The owner chose the optional-provider-interface resolution.
`provider.ModelReporter { Session; CurrentModel() string }` added; the record
asks the session for its model outside `m.mu`, so no provider mutex nests
inside the manager lock. `httpagent.session.CurrentModel` prefers the model the
session explicitly recorded and falls back to a new optional
`httpagent.DialectDefaultModel`, which kilo and opencode implement over the
`fallbackModel()` they already had.

**The first implementation was wrong, and the unit tests could not see it.** It
asked `s.ds` — the per-session `DialectSession`, which for kilo is
`kilo.httpSession` and implements no such method. It compiled, every
`internal/session` test passed (they use their own fake), and a live `hi`
produced a record with **no model at all**:

```json
{"msg":"turn latency","provider":"kilo","turn_ms":10238,"ttft_ms":5109,
 "cold":true,"context_used":14560}
```

The engine default belongs to the provider-wide dialect. Fixed to `s.p.dialect`
— the same field `ProviderID()` and `ModelCatalog()` already use — and pinned
by `internal/provider/httpagent/currentmodel_test.go`, which was verified to
fail against the defect:

```text
F. s.p.dialect reverted to s.ds (the live defect)
   FAIL TestCurrentModelReadsTheProviderDialectNotTheSessionView
        CurrentModel() = "", want kilo/kilo-auto/balanced
```

Two further breakages pinned the record side:

```text
D. record uses Meta.Model only (the pre-amendment code)
   FAIL TestRecordNamesTheModelTheSessionIsActuallyRunningOn
        model = <nil>, want kilo/kilo-auto/balanced

E. fall back to the provider id when no model is known
   FAIL TestRecordOmitsTheModelWhenTheSessionCannotReportOne
        model = lat on a session that reports none; it must be absent
```

**Live, after the fix** — one `hi`, kilo 7.5.6, client named no model:

```json
{"msg":"turn latency","session_id":"256a12d7-c2c2-4806-b943-fac3183b575c",
 "provider":"kilo","turn_ms":6598,"model":"kilo/kilo-auto/balanced",
 "ttft_ms":5165,"cold":true,"context_used":14472}
```

**Coverage is two providers of five.** grok, goose and codex track no default
model, so they implement nothing and the field stays absent. That is a
provider-side gap this phase names, not one it closes.

**Three cold kilo turns, same host, same binary, same model:** ttft 5167 /
5109 / 5165 ms, turn 13670 / 10238 / 6598 ms. Cold TTFT is strikingly stable
across runs while total turn time varies 2x — the wait before the first token
is a fixed cost, and it is the 5.1s that Phase 6 has to account for.

```text
go test ./internal/... -count=1   -> ok (whole tree)
make pre-add-check                -> 722 file(s) clean
```

### Phase 3 + steps 7.5 and 7.9 — 2026-09-03, complete

7.5 landed with Phase 3 rather than before it, because the three new pins have
nothing to compare against until their version source exists.

**3.1** kilo `KnownGoodVersion` 7.4.23 -> **7.5.6**; opencode 1.18.21 ->
**1.18.26**. Both constants now cite their Phase 1 fixture directory and its
frame counts (3.4).

**3.2** New `version.go` for grok (**1.0.13**), goose (**1.48.0**) and codex
(**0.152.1**), each citing its fixture.

**3.3** Every pin warns and never refuses, stated in a comment above each
constant, including the instruction not to harden it into a gate without a
decision record.

**7.5 (amended)** Version readers, one per transport:

| provider | source | file |
| --- | --- | --- |
| goose | `agentInfo.version` (standard ACP) | `acphttp/version.go` |
| grok | `_meta.agentVersion` (vendor) | `acpagent/version.go` |
| codex | `userAgent` prefix | `codex/version.go` |

`provider.SameVersion` is the shared comparison: semantic, tolerant of a
leading `v` and of pre-release/build metadata, and **never equal on
unparseable input** — including two identical unreadable strings. A pin that
reports agreement it never established is worse than no pin.

**7.9** `acphttp.postJSON` is hooked, so the POST /acp control plane is
captured alongside the websocket. The goose fixture was re-recorded from a
live `hi`: 14 frames, the first three being `initialize` request, `initialize`
response and the `session/new` response — none of which the previous 15-frame
websocket-only capture contained.

### Verification

**All five engines started under one isolated daemon, all five matched their
pin, zero drift warnings:**

```text
INFO  kilo engine version          version=7.5.6
INFO  opencode engine version      version=1.18.26  known_good=1.18.26
INFO  engine version               provider=goose   version=1.48.0
INFO  engine version               provider=grok    version=1.0.13
INFO  codex engine version         version=0.152.1
```

Engines confirmed up from the log: `provider.kilo-http`,
`provider.opencode-http`, `provider.goose-acphttp`, `provider.grok`,
`provider.codex`.

**Six deliberate breakages, each verified to have landed, each restored from a
`cp` backup:**

```text
G. SameVersion treats two unparseable strings as equal
   FAIL TestSameVersionRefusesUnparseableInput
        SameVersion("", "") = true, want false
        SameVersion("nonsense", "nonsense") = true, want false

H. read only agentInfo.version (step 7.5 exactly as originally written)
   FAIL .../grok:_vendor__meta.agentVersion,_no_agentInfo_at_all
        engineVersionOf = "", want "1.0.13"

I. codex returns the userAgent token without checking it is a version
   FAIL TestVersionFromUserAgent
        versionFromUserAgent("codex_cli_rs/notaversion (x)") = "notaversion", want ""

J. codex pin bumped to 0.153.0 with no fixture
   FAIL TestPinMatchesTheFixtureItCites
        testdata/wire/0.153.0 does not exist

K. the pre-7.9 goose fixture (POST frames dropped, websocket only)
   FAIL TestPinIsCorroboratedByItsFixture
        no agentInfo.version: the fixture does not cover the initialize handshake

L. goose pin bumped to 1.49.0 with no fixture
   FAIL TestPinIsCorroboratedByItsFixture
        testdata/wire/1.49.0/frames.jsonl: no such file or directory
```

Breakage H is the one worth keeping: it is step 7.5 as the plan originally
specified it, and it leaves grok — the provider whose performance prompted this
record — with no version at all.

```text
go build ./...                            -> ok
go test ./internal/... ./cmd/... -count=1 -> ok
make pre-add-check                        -> 733 file(s) clean
```

### Deviations

**2026-09-03 — `agentInfo.version` is not the version source for grok.** Step
7.5 named it as the single source for both grok and goose.

*Evidence.* grok 1.0.13 sends no `agentInfo`: zero occurrences across all 247
frames of its full-turn fixture, which contains the complete `initialize`
result. The ACP SDK types the field as optional
(`acp-go-sdk@v0.13.5/types_gen.go:2336`). grok reports
`result._meta.agentVersion` instead. goose does send the standard field,
confirmed by driving the engine. codex is not ACP and was never covered by 7.5
at all; its version is in the `initialize` `userAgent`, whose shape is
`<originator>/<CARGO_PKG_VERSION>` per
`codex-rs/login/src/auth/default_client.rs:164-170`.

*Not worked around.* No pin was dropped, and no version was inferred from
`<bin> --version`, which measures the binary on PATH rather than the engine
that is running — wrong for codex, whose app-server can be a managed daemon of
a different build.

*Resolution, chosen by the owner:* read each engine's own source. See the
MADR's ninth amendment.

**2026-09-03 — the goose fixture had no handshake.** `acphttp` initializes over
`POST /acp` (`conn.go:109`) while the MADR 0137 capture hooked only the
websocket, so the fixture the sixth amendment added for exactly this purpose
could not answer the question above — it had to be settled by driving the
engine live.

*Resolution, chosen by the owner:* step 7.9, added to the plan. `postJSON` is
hooked under the same environment gate and redaction, and the fixture is
re-recorded with the handshake. Redaction verified at zero occurrences of the
operator's username and zero `Users/` paths; 0 malformed JSON lines.

*Files added to this phase's scope:* `internal/provider/version.go`
and its test, `internal/provider/acphttp/{conn.go,provider.go,spec.go,version.go,conn_test.go}`,
`internal/provider/acpagent/{acpagent.go,version.go,version_test.go}`,
`internal/provider/codex/{provider.go,version.go,version_test.go}`,
`internal/provider/{grok,goose}/{version.go,version_test.go}` and their Spec
literals, `internal/provider/kilo/dialect_test.go`,
`internal/provider/opencode/version_test.go`, and the goose fixture.

**Observed, not acted on.** grok's `initialize` `_meta` carries far more than a
version: `modelState.currentModelId` (`grok-4.6`) with `availableModels` and
per-model reasoning-effort rungs, `availableCommands`, `mcpServers`,
`cancelRewind`, `sessionRecap` and `voiceMode`. The model catalog half is
already harvested (`acpagent.go:502`); the rest is unconsumed. Recorded for
step 7.7's recorded-not-adopted list rather than changed here.

**Not done in this phase.** The pin-drift *warning* paths for grok, goose and
codex have unit tests only for the version READ, not for the warn/info/silent
branches. The branches were exercised live — all five matched, so only the
info branch ran — and the warning text is shared with the kilo/opencode paths
that do have matrix tests. A drift matrix per transport belongs with Phase 5.

### Phase 4 — 2026-09-03, complete

**4.1 F5 — grok prewarm re-arm.** `defer p.EnsureWarm()` removed from
`acpagent.Start`. Re-arming there put a full ~3.8 s replacement spawn — process
launch, ACP initialize, model-catalog harvest — in flight at the same moment as
the user's first prompt. The spare is for the NEXT session; there is no reason
to build it while someone is waiting. The daemon still arms the cold first
spare at startup (`daemon.go:226`), which is where `EnsureWarm`'s own doc
comment says the initial call belongs.

**4.2 F2 — `available_commands` dedupe, all four providers.**
`event.CommandDeduper` in `internal/event/dedupe.go`, used at every emit site:
`acpagent` (grok), `acphttp` (goose), and a shared `emitCommands` helper on
kilo's and opencode's `httpSession`, which covers both the live and
catalog-down fallback paths in each. The first advertisement always goes out,
including an empty one.

**4.3(a) F6 — notice dedupe, in the session event pump.**

**4.4 F1 — `ObserveCredentialStoreCachedNonBlocking`.** A cold cache returns
`RealityUnknown` at once and starts at most one background probe;
`backupProjection` uses it, so the phone-triggered `providers.list` path no
longer reaches a ~1.4 s `codex doctor --json`. Recovery and `CheckBackend` are
untouched — they are not phone-triggered and MADR 0136's classification needs a
real observation.

**4.5 F4 — ws dispatch, decided per handler.** `session.list`,
`session.set_mode` and `session.set_config` moved to `dispatchAsync`; each
reaches provider state. `session.cancel`, `session.pending_asks` and
`oauth.cancel` stay inline, each with a comment saying why: they are
control-plane operations, and `dispatchAsync` is bounded by
`maxAsyncPerClient = 8`, so a cancel that queues behind eight prompts cannot
cancel them.

### Fail-first evidence — nine breakages, each verified to have landed and each restored from a `cp` backup

```text
M. re-arm removed entirely
   FAIL TestSpareIsRearmedAtTurnEndNotTurnStart   wanted 1 "prewarm failed", got 0
   FAIL TestAFailedTurnStillRearmsTheSpare        wanted 1 "prewarm failed", got 0

N. re-arm on the happy path only
   FAIL TestAFailedTurnStillRearmsTheSpare        wanted 1 "prewarm failed", got 0

O. arming at turn START (the pre-0137 behaviour)
   FAIL TestSpareIsRearmedAtTurnEndNotTurnStart
        the spare was armed 1 time(s) while the turn was still running

P. session.set_config back on the read loop
   FAIL TestSlowSetConfigDoesNotStallTheConnection
        the prompt was not answered while set_config was blocked

Q. notice dedupe removed from the pump
   FAIL TestRepeatedNoticesAreSuppressed          got 7 notices, want 3

R. the !ev.Replay guard dropped
   FAIL TestAReplayDoesNotPoisonTheDeduper        got 1 notices, want 2

S. the non-blocking probe made blocking again
   FAIL TestNonBlockingProbeRunsNoBinaryOnAColdCache
        the binary ran before the call returned

S2. the in-flight coalescing flag removed
   FAIL TestNonBlockingProbeCoalescesConcurrentCallers
        20 concurrent callers started 3 probes

T. the deduper aliases the caller's slice
   FAIL TestDeduperDoesNotAliasTheCallersSlice
        a changed description was suppressed
```

Breakage O is the one that matters for 4.1: it is the behaviour that shipped,
and it arms the spare while the user's first turn is still running.

```text
go build ./...                            -> ok
go test ./internal/... ./cmd/... -count=1 -> ok
make pre-add-check                        -> 743 file(s) clean
```

### Deviations

**2026-09-03 — the notice dedupe went in the pump, not at 42 emit sites.** Step
4.3(a) said "applies at every `TypeNotice` emit site".

*Evidence.* There are **42** `TypeNotice` emit sites across six packages
(`kilo`, `opencode`, `codex`, `acpagent`, `acphttp`, `httpagent`) plus
`internal/session`. Deduping at each would need a deduper field on six session
types and 42 guards, and a new emit site added later would silently opt out.

*Resolution, chosen by the owner:* one guard in the session event pump, where
every provider's events converge and the per-session `entry` state already
lives. `internal/session/manager.go` added to this phase's file list. See the
plan's amended 4.3(a).

**2026-09-03 — a re-arm on the happy path only would have been wrong.** Step
4.1 named the emit site of `turn_complete` / `status:"idle"` as the place to
call `EnsureWarm`. Putting it there skips the error and cancel exit paths,
which `return` earlier — so a session whose first turn failed would be left
with no spare at all, exactly when the engine is already having trouble. It is
in the turn goroutine's deferred cleanup instead, which every exit path
reaches. Breakage N is that variant, pinned.

**2026-09-03 — the replay assertion first written for 4.3 was measuring
something else.** "Three identical replayed notices produce three history
entries" failed before the guard was even involved: `appendHistoryLocked`
already de-duplicates any replayed event against existing history
(`manager.go:202-208`). The test was rewritten to assert what `!ev.Replay`
actually protects — that a replayed notice does not update the deduper's state
and thereby silence the next genuine live one. Breakage R pins that.

### Not done in this phase

**The `backupProjection` call site has no unit test.** The non-blocking probe
itself is covered by two tests with fail-first evidence, but the wiring — that
`backupProjection` calls the non-blocking variant rather than the blocking one
— is not, because reaching it needs a coordinator that lives in the daemon
package. Breakage S proves the function; it does not prove the call site. This
is the same class of gap that let Phase 2's `s.ds` defect through unit tests
and only surface live, and it is named here rather than left implicit. Phase 5
exercises the path on a real host.

**4.2 has no live confirmation yet.** The per-provider tests drive each
provider's real emit path, but the 22-repeats-per-turn figure that motivated
F2 was measured on grok live. Phase 5 should re-measure it.

### Phase 5 — 2026-09-03, complete

30 turns: three sessions per provider, two sequential turns each, on the host
that produced the original report, with the Phase 2 instrumentation reading
every one.

### 5.2 Cold and warm, kept separate — and a third population the plan did not anticipate

`cold` is absent entirely when a provider reports no usage at all. That is
**unknown**, not warm, and counting it as warm would invent a population.

| provider | class | n | ttft_ms | median |
| --- | --- | --- | --- | --- |
| kilo | cold | 8 | 4012, 4280, 4308, 4338, 4338, 4388, 4432, 5348 | **4338** |
| opencode | cold | 1 | 2582 | 2582 |
| opencode | **warm** | 5 | 969, 1129, 1311, 1329, 1528 | **1311** |
| goose | cold | 5 | 1164, 1305, 1503, 1847, 8337 | **1503** |
| goose | no usage | 1 | — (turn produced no output) | — |
| codex | no usage | 6 | 1111, 2127, 2165, 2907, 2933, 2975 | 2536 |
| grok | no usage | 4 | 1362, 1426, 32461, **155096** | 16943 |

**Only opencode ever reported a cache read.**

| provider | turns reporting a cache read |
| --- | --- |
| opencode | **5 of 6** (cache_read 12992 every time) |
| kilo | 0 of 8 |
| goose | 0 of 6 |
| codex | 0 of 6 |
| grok | 0 of 4 |

### 5.3 The verdict, stated plainly

**The gap partially closed, and the remaining cost is not in mcremote.**

* **opencode is the control, and it settles the question.** It is the one
  provider whose cache works, and it is the one provider that is fast:
  **1311 ms warm median against 2582 ms cold**, a 2x difference on the same
  binary, model and host within seconds of each other. That is the cold/warm
  split this record predicted, measured directly rather than inferred.
* **kilo never gets a warm turn.** Eight turns, eight cold, `cache_read`
  absent on every one — including the second turn of a session whose first
  turn had just run. Its TTFT is correspondingly flat and high: 4012-5348 ms,
  a 1.3x spread. Against the MADR's historical 0.6-2.9 s this is still a
  regression; against the regressed 5-18.6 s it is the bottom of that range,
  consistently, with the 18 s tail gone.
* **goose and codex are in the historical band** — 1164-1847 ms and
  1111-2975 ms — with one 8337 ms goose outlier on a first-ever turn.
* **grok is bimodal and not explained by anything in mcremote.** Two turns at
  ~1.4 s and two at 32 s and 155 s.

Nothing here supports a claim that the regression is fixed. What the data does
establish is **where it is not**: the two providers whose engines report a
working prompt cache or a warm path are fast, and the ones that report no cache
read are slow in proportion. Phase 6 owns the cache question, and it now has a
control to compare against.

### 5.5 The hang was reproduced, and it is upstream

grok session `c9a285f0`, the 155 s turn. Captured before anything was
restarted:

```text
20:27:46.572  user_message "hi"
20:27:46.572  session_status
              ...155 seconds of complete silence...
20:30:21.669  thought_chunk  "The"
20:30:21.669  thought_chunk  " user just said \"hi\" - this is a simple greeting..."
20:30:21.677  assistant_message_chunk  "Hi"
20:30:21.677  assistant_message_chunk  ". What do you want to work on?"
20:30:21.770  turn_complete
```

The entire reply arrived in **100 ms**, after 155 s of nothing. mcremote had
nothing to deliver for 155 seconds, so no amount of transport work shortens it.
The daemon behaved correctly throughout: the turn completed, `turn_complete`
was emitted, and the Phase 2 record captured `ttft_ms=155096 turn_ms=155197`.

**Worth noting for step 7.8:** the only thing on the wire during that silence
was a single `unhandled session update` at 16 s in — an unrouted grok
extension. Four such updates across the run. If any of them carries progress or
queue state, routing it (7.8) would turn a 155 s blank screen into a 155 s wait
the user can see the reason for. That does not make the turn faster; it makes
the silence legible.

### 4.2 confirmed live — the strongest result in this phase

`available_commands` events, counted from each session's own history:

```text
provider   sessions   available_commands per session
kilo          12                1
opencode       3                1
grok           3                1
goose          3                0
codex          3                0
```

**Exactly one per session, grok included** — the provider measured at **22 in a
single `hi` turn** before Phase 4. goose and codex show zero because they
advertise through a different event, not because anything was suppressed.

### 5.4 Codex deprecation notice — gone, and F6(b) is already resolved

Zero notices in any codex session, and zero occurrences of "deprecat" anywhere
in the daemon log across the whole run. `~/.codex/config.toml` has no
`[features]` block — the operator fix recorded at the top of this record is
still in place — and `codex features list` reports `hooks stable true`, so the
capability is on with no legacy alias attributed. **F6(b) needs no further
action.**

### Deviations

**2026-09-03 — measured on an isolated daemon, not via `make install` and the
phone.** Step 5.1 says to install, restart and prompt from the phone.

*What was done instead.* The same binary, on the same host, against the same
five engines and the same config, run as an isolated daemon on port 7631 with
its own data dir, driven by a local WebSocket client speaking `mcremote.v1`.

*Why.* `make install` replaces the operator's running daemon, which their phone
is connected to. That is a change to the owner's environment, and this plan
does not authorise it — nor does the measurement need it. **The install is
offered to the owner rather than taken.**

*What this does not cover.* The phone client itself: rendering, reconnect
behaviour, and anything the Flutter app does with the events. No claim is made
about those. Everything measured here is daemon-side.

**2026-09-03 — the harness reported six false hangs before it reported any
number.** The first run said `NO TURN END` on all six kilo turns. The daemon
log showed all six had completed in 4-7 s: the harness read `type` at the top
of the envelope payload, where the event is actually nested under `event`
(`protocol.EventPayload`). Fixed and re-verified against a known-good kilo turn
before any measurement was recorded.

*This is worth writing down.* Step 5.5 asks the operator to watch for a hang,
and the first thing this phase produced was a hang that did not exist. A
measurement harness is code and fails like code — the daemon's own record is
what distinguished the two, which is precisely what Phase 2 was built for.

**Not covered.** Three cold AND three warm per provider was not achievable as
specified. kilo, goose, codex and grok produced no warm turns at all in 30
turns, because their engines report no cache read — which is the finding, not a
gap in the method. opencode is the only provider with both populations, and it
has 1 cold and 5 warm rather than 3 and 3.

### Phase 6 — 2026-09-03, complete. It overturned this record's central thesis.

### 6.1 The cache was never broken. The instrument was blind.

Three turns of one kilo session, read at the wire:

| turn | input | cache.read |
| --- | --- | --- |
| 1 | 14435 | 0 |
| 2 | **212** | **14336** |
| 3 | 13651 | 2304 |

Phase 2 recorded **all three as `cold=true`**. `internal/provider/kilo/usage.go`
parsed `Input`, `Output`, `Reasoning`, `Cache.Read` and `Cache.Write`, used them
to compute a context total, and emitted `event.Usage{Used, Size}` — discarding
all five. `cold` is `CacheRead == 0`, so for kilo it could only ever be true.

Four of five engines report cache reads. mcremote read it for one.

| provider | engine reports | mcremote mapped it | now |
| --- | --- | --- | --- |
| opencode | `cache.read` | yes | yes |
| kilo | `cache.read` | **no** — parsed then dropped | fixed |
| grok | `cachedReadTokens` on `_x.ai/session_notification` | **no** | fixed |
| codex | `cachedInputTokens` on `thread/tokenUsage/updated` | **no** | fixed |
| goose | nothing | n/a | `cold` now absent, not false |

**A second, independent defect found on the way.** codex's
`thread/tokenUsage/updated` sends `total` as an OBJECT with
`modelContextWindow` nested inside `tokenUsage`. mcremote decoded both as flat
ints, so `json.Unmarshal` failed, the `err == nil` guard skipped the emit, and
**codex published no usage event at all** — no `/context` on the phone for
codex users, and no cold/warm signal. It failed silently: the event simply never
existed.

### The re-measurement, with the instrument working

15 turns, three per provider, three sequential turns per session:

| provider | turn | cold | ttft_ms | fresh input | cache_read |
| --- | --- | --- | --- | --- | --- |
| kilo | 1 | false | 6068 | 13667 | 768 |
| kilo | 2 | false | 5173 | 1547 | 14336 |
| kilo | 3 | false | **4544** | **337** | 15616 |
| opencode | 1 | false | 3301 | 40 | 12992 |
| opencode | 2 | false | 4877 | 95 | 12992 |
| opencode | 3 | false | 4360 | 85 | 13056 |
| codex | 1 | false | 2852 | 16312 | 16128 |
| codex | 2 | false | 1427 | 33493 | 32256 |
| codex | 3 | false | 1672 | 50694 | 49280 |
| grok | 1 | false | 1239 | 19042 | 11776 |
| grok | 2 | false | **12094** | 19105 | 18944 |
| grok | 3 | false | **857** | 19169 | 19072 |
| goose | 1-3 | *absent* | 1243, 750, 644 | — | — |

**Not one cold turn in fifteen.** Every turn on every cache-reporting provider
read a warm prefix.

### 6.2/6.3 The finding, and why it needed no quota to reach

**Prompt weight is not the cost, and the data settles it without an arm.**

* kilo turn 3: **337 fresh input tokens, 15616 cached — and still 4544 ms to
  first token.** There is no prompt weight left to remove. Three hundred
  tokens of fresh input cannot take four and a half seconds.
* grok turn 3: **19169 input, 19072 cached — 857 ms.** Five times kilo's total
  context, a fully warm prefix, and five times faster.
* codex turn 3: **50694 input, 49280 cached — 1672 ms.** The largest context
  measured anywhere in this record, and among the fastest turns.

Removing the `magictools` MCP server or the `@kilocode/plugin` would shrink a
prefix that is already cached and already not being paid for. The arms were not
run, and **that is the finding, not an omission**: an experiment whose outcome
is determined by data already in hand is not worth the owner's quota.

For the record, the configuration that would have been varied:
`~/.kilo/_kilo.jsono` has one MCP server (`magictools`, remote,
`http://localhost:48080/mcp`, reachable — HTTP 400 to a bare GET, so the port is
live) and `~/.kilo/package.json` has one plugin (`@kilocode/plugin` 7.4.20).
**Neither was touched**, per 6.3.

### What kilo's latency actually is

Two causes, both upstream, both outside mcremote:

1. **`kilo-auto/balanced` routes to different upstream models within one
   session.** The three-turn capture shows `moonshotai/kimi-k3` and
   `deepseek/deepseek-v4-flash-0731` serving different turns under the one
   router alias. Turn 1 read only 768 cached tokens against 13667 fresh — a
   near-cold prefix on a session that had just started, because the backend it
   landed on had no cache for it.
2. **The backends the router picks are slow.** kilo's warmest turn is its
   slowest per token: 4544 ms for 337 fresh tokens. grok answers in 857 ms with
   19072 cached tokens on the same host, minutes apart.

**Recommendation, for the owner to accept or decline — no change made.** If
kilo's first-token latency matters more than the auto-router's cost or capability
selection, naming a specific model instead of `kilo-auto/balanced` would remove
both causes at once: a fixed backend keeps one cache warm across turns, and the
model becomes a known quantity rather than a per-turn lottery. This record's
accepted constraint is that providers run on their own default model, so this
is a recommendation and nothing was changed.

### 5.3 is superseded

The Phase 5 verdict said "kilo never gets a warm turn" and treated opencode as
the control proving a cold/warm split. Both were artefacts of the blind
instrument. opencode was not a control; it was the only provider whose usage
mapping was finished, which is also why it was the only one that could be seen
to be fast. The Phase 5 entry is left in place rather than deleted: what was
measured, and why it was wrong, is more useful than a clean number.

### Fail-first evidence — five breakages, each verified to have landed and restored from a `cp` backup

```text
U. kilo emits {Used, Size} only (the code as it shipped)
   FAIL TestUsageCarriesTheCacheSplit
        CacheRead = 0, want 14336: a warm turn is being reported as cold

V2. codex modelContextWindow read from the wrong nesting level
   FAIL TestTokenUsageDecodesTheShapeCodexActuallySends
        modelContextWindow decoded as 0: it is nested inside tokenUsage

W. grok turn_completed ignored again
   FAIL TestXAITurnCompletedCarriesGroksCacheSplit   no usage event emitted

X. cold reported unconditionally (the blind instrument)
   FAIL TestColdIsAbsentWhenTheProviderReportsNoCacheAccounting
        cold = true on a provider that reports no cache accounting
```

A fifth breakage — reverting codex's decode to the flat ints it had — **no
longer compiles**, because the mapping code uses the object's fields. That is
the strongest available guard: the shape cannot silently regress.

**One of these tests was itself wrong first.** Breakage V produced no failure at
all, because the codex test had duplicated the decode struct rather than using
the production one — the same class of defect as the original, and exactly what
let it survive. The struct is now a shared named type, `codexTokenUsageParams`,
used by both the handler and its test, and V2 above is the re-run against it.

```text
go test ./internal/... ./cmd/... -count=1 -> ok
make pre-add-check                        -> 748 file(s) clean
```

### Deviations

**2026-09-03 — Phase 6 wrote code, which it is specified not to do.** The phase
is investigation-only.

*Evidence.* The investigation's first result was that Phase 2's cold/warm signal
is wrong for four of five providers, which makes every latency number in this
record unattributable and both prior phases' conclusions unsound.

*Not worked around.* The alternative — report the defect and leave the
instrument blind — was put to the owner explicitly and declined.

*Resolution, chosen by the owner:* map every engine's cache accounting, then
re-measure. Files: `internal/provider/kilo/{usage.go,session.go}`,
`internal/provider/codex/{session.go,tokenusage.go}`,
`internal/provider/acpagent/{subagents.go,xaiusage.go}`,
`internal/session/turnlatency.go`, and tests alongside each. See the MADR's
second correction.

**2026-09-03 — 6.2's arms were not run.** Quota was approved for them.

*Reason.* The re-measurement answered the question the arms exist to answer.
kilo's warmest turn pays 337 fresh input tokens and still takes 4544 ms;
removing an MCP server would shrink a prefix that is cached and not being paid
for. Spending approved quota on an experiment whose outcome is already
determined is not diligence.

*What is therefore not established.* The composition of the ~14k-token prefix
is still unattributed — how much is the agent system prompt, how much is
`magictools`' tool definitions, how much is the plugin. That question is now
about cost and context headroom, not latency, and it is worth answering only if
one of those becomes the concern.

### Phase 7 (except 7.5, 7.9) — not yet started
