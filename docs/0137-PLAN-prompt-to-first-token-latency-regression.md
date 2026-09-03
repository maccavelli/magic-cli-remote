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

1.1 Add `scripts/capture-wire/`. It connects to one provider engine's event
stream, writes every frame verbatim to `frames.jsonl`, and writes `meta.json`
naming provider, binary version and capture date. Transport by flag:
`-kind=sse -url=…` for kilo/opencode/goose, `-kind=acp -bin=…` for grok/codex.

1.2 Capture one `hi` turn each for kilo **7.5.6** (SSE), opencode **1.18.26**
(SSE), goose **1.48.0** (ACP/HTTP) and codex **0.152.1** (app-server
JSON-RPC). The codex capture must include the session-start notifications, so
the `deprecationNotice` of F6 is in the fixture rather than described from
memory.

1.3 **grok 1.0.13 only after the owner confirms quota.** Standing constraint:
no provider quota is spent on a test without asking. If declined, Phase 3 still
adds grok's pin but its comment reads "no fixture — quota withheld", so the gap
is visible rather than implied.

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
  not become per-session noise. Applies at every `TypeNotice` emit site.
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

7.5 **F10a — read `agentInfo.version` from the ACP handshake** and use it as
the version source for the grok and goose pins added in Phase 3, replacing
whatever ad-hoc source those pins would otherwise need.

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

**Done when:** 7.1-7.6 land with tests, and 7.7 is confirmed as a written
decision rather than an oversight.

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

### Phases 2-7 — not yet started
