---
status: in-progress
date: 2026-09-03
associated-madr: "0137-MADR-prompt-to-first-token-latency-regression.md"
---

# Implement: restore first-token latency — delivery, instrumentation, pins, and per-provider optimisation

Associated MADR: [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md)

## Goal

Make `hi` answer in about a second again, and make the next regression
detectable by the daemon rather than by the operator. In order: fix the turns
that never arrive, measure the turns that arrive slowly, then remove the
avoidable cost — across every provider, with pins that match reality.

## Scope

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

1.2 Capture one `hi` turn each for kilo **7.5.6**, opencode **1.18.26**,
goose **1.48.0**, codex **0.152.1**.

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

### Phase 4 — four specific optimisations

**Files:** `internal/provider/acpagent/acpagent.go`,
`internal/provider/acpagent/session.go`,
`internal/provider/codex/store_reality.go`,
`internal/provider/codex/logout.go`, `internal/ws/server.go`, plus tests
alongside each.

4.1 **F5 — prewarm re-arm off session create.** Delete `defer p.EnsureWarm()`
from `Start` (`acpagent.go:775`); call `p.EnsureWarm()` from the turn-end path
in `session.go` where `TypeTurnComplete` and the `status:"idle"` event are
emitted (lines 413-425). *Test:* `EnsureWarm` is not called between `Start`
returning and the first `turn_complete`, and is called exactly once after it.

4.2 **F2 — `available_commands` dedupe.** In `acpagent/session.go:1471` keep
the last emitted list per session and skip the emit when the new list is equal
(same names, descriptions, hints, in order). *Test:* ten identical updates
produce one event; changing one description produces a second.

4.3 **F1 — the Codex probe leaves the phone-triggered path.** Add
`ObserveCredentialStoreCachedNonBlocking` to `store_reality.go`: return the
cached value when fresh, else return `RealityUnknown` immediately and start at
most one background refresh. `backupProjection` (`codex/logout.go:241`) calls
it. Synchronous probing is unchanged on the recovery and `CheckBackend` paths,
which are not phone-triggered. *Chosen over* narrowing `doctor --json` or
widening the cache window: both leave a ~1.4 s network-dependent call reachable
from `providers.list`, and this removes it outright without weakening MADR
0136's classification. *Test:* `AuthStatus` with a cold cache invokes no binary
and returns the unknown/unsupported projection rather than a fabricated one.

4.4 **F4 — inline ws handlers, decided per handler.** In `internal/ws/server.go`
move `session.list`, `session.set_mode` and `session.set_config` to
`dispatchAsync`; they can touch provider state. **Keep** `session.cancel`,
`session.pending_asks` and `oauth.cancel` inline, with a comment stating why:
they are control-plane operations that must not queue behind
`maxAsyncPerClient = 8`, and a cancel that waits for a slot defeats its purpose.
*Test:* a 2 s block in `session.set_config` does not delay a following
`session.prompt` on the same connection.

**Verification:** `go test ./internal/provider/... ./internal/ws/... -count=1`;
each of 4.1-4.4 has a test that fails against pre-change code, output recorded.

**Done when:** all four land with fail-first evidence recorded.

### Phase 5 — verify on the reporting host

**Files:** none; verification only.

5.1 `make install`, restart, then three `hi` turns each on kilo and grok from
the phone.

5.2 Read `ttft_ms` and `cold` from the Phase 2 records. Report cold and warm
separately; never average across them.

5.3 Compare against the MADR: historical 0.6-2.9 s, regressed 5-18.6 s, warm
direct-engine 0.79-0.97 s, cold ~14 k-token prefill. State plainly whether the
gap closed, partially closed, or did not. If warm turns are fast and cold ones
are not, say exactly that rather than claiming a fix.

5.4 Watch for the unreproduced hang. If a turn yields neither output nor error,
capture the `turn latency` record, the session `history.json`, and the engine's
own view of that session **before** restarting anything.

**Done when:** three cold and three warm measurements per provider are in the
execution record, with the verdict stated honestly.

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
* Ten identical `available_commands_update` frames yield one event.
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
its fixture). Phase 5 depends on Phase 2 (it reads the `turn latency` record).
Phase 6 depends on Phase 2 and on Phase 5's host numbers. Phase 4 is
independent of all of them and may land at any point. So the order is
1 → 2 → 3 → 5 → 6, with 4 inserted wherever convenient.

**What this plan will not do.** It will not remove the operator's MCP server or
plugins, will not change or pin a provider's default model (an accepted
constraint of the MADR, not merely a scoping choice), and will not upgrade or
downgrade a provider binary. Where those turn out to be the dominant cost,
Phase 6 reports the number and the decision stays with the owner.

## Execution Record

Not started. `status: proposed` — awaiting approval to execute.
