# CLI capability uptake: implementation plan

**Status:** Proposed
**Date:** 2026-07-27
**Decision:** [MADR 0037](./0037-MADR-cli-capability-uptake.md)
**Verified targets:** grok 0.2.112 (9bbd559437), codex-cli 0.145.0,
opencode 1.18.7, goose 1.44.0. Re-probe phase 0 before accepting newer versions.

## Goal and non-goal

Take the four grounded gaps from the upstream-release review: grok reasoning
effort (typed, composing with model override), opencode plugin isolation, goose
origin alignment, and a false comment about orphan reaping.

**Non-goals.** No grok worktree flag — it does not exist on the agent path. No
codex remote `ws://` transport, no codex multi-agent tree, no daemon-managed
worktrees: each needs its own MADR (0037 §2.5). No work on codex process
supervision, goose builtins or env passthrough — all already implemented
(0037 §1.7).

## Dependency order

All four are independent. Phase 0 pins the external facts the other phases
assume, and phase 3 is the only one whose correctness depends on an external
policy we do not control.

```
Phase 0 (pin CLI contracts) ─> Phase 1 (grok effort) ─┐
                               Phase 2 (opencode pure)├─> Phase 5 (docs + verify)
                               Phase 3 (goose origin) ─┤
                               Phase 4 (comment fix) ──┘
```

---

## Phase 0 — P1: pin the CLI contracts with live tests

Per AGENTS.md, a decision resting on external CLI behaviour needs probe evidence
and a live-tagged test — CLI behaviour changes silently.

1. Extend the existing `live_grok` tests with one that runs
   `grok agent --help` and asserts `--reasoning-effort` is present. This is the
   flag D1 depends on; if grok moves it, the daemon starts emitting an argument
   the binary rejects and every session fails to start.
2. Add a `live_goose` test asserting `goose serve --help` still lists
   `--allowed-origin`, and record the default-origin wording. Phase 3's
   correctness depends on goose's default loopback-origin policy.
3. Add a `live_opencode` test asserting `opencode serve --help` lists `--pure`.
4. Record the probed versions in the test output so a failure names what moved.

**Gate:** phases 1-3 assume these flags exist on these versions. Do not
implement against a flag this phase cannot find.

---

## Phase 1 — P1: typed `reasoning_effort` for grok

**Files:** `internal/provider/acpagent/acpagent.go`,
`internal/provider/grok/grok.go`, `internal/config/config.go`,
`internal/daemon/daemon.go`

1. Add `ReasoningEffort string` to `acpagent.Config`, documented as an
   engine-level setting passed to `grok agent`.
2. In `grok.go` `defaultArgs`, append `--reasoning-effort <value>` when the
   field is non-empty. Place it alongside the existing `-m` handling so the
   ordering stays stable and readable.
3. **Thread it through `ModelArgs`** — the whole point of the decision:

   ```go
   ModelArgs: func(cfg Config, model string) []string {
       return defaultArgs(Config{
           AlwaysApprove:   cfg.AlwaysApprove,
           Model:           model,
           ReasoningEffort: cfg.ReasoningEffort,
       })
   },
   ```

   Without this the typed field reproduces the raw-`args` bug it exists to fix
   (MADR 0037 §1.2). Update the adjacent comment, which currently explains only
   why `cfg.Args` is dropped.
4. Add `ReasoningEffort string \`mapstructure:"reasoning_effort"\`` to the grok
   provider config and wire it in `daemon.go` next to `Args`/`Model`.
5. Validation: reject only a value that is non-empty after trimming but becomes
   empty — i.e. whitespace-only — which would emit a bare flag. Do **not**
   validate against an enum: grok documents none, and a hardcoded list breaks
   when grok adds a tier (0037 §2.1).

**Tests:**

- `defaultArgs` with effort set produces `agent --no-leader --reasoning-effort high stdio`.
- `defaultArgs` with effort empty is byte-identical to today (the no-regression
  guard).
- **`ModelArgs` preserves the effort** — the regression test for the composition
  bug. Assert both `-m` and `--reasoning-effort` appear.
- Config parse/validate: a valid value round-trips; whitespace-only is rejected
  with a message naming `providers.grok.reasoning_effort`.
- Live (`live_grok`): a session starts successfully with an effort set, proving
  grok accepts the argument rather than exiting on an unknown flag.

**Acceptance:** setting `providers.grok.reasoning_effort: high` reaches the
process, and survives a session that also overrides the model.

**Rollback:** remove the field; `defaultArgs` falls back to today's vector.

---

## Phase 2 — P1: `pure` for the opencode engine

**Files:** `internal/provider/opencode/http.go`, `internal/config/config.go`,
`internal/daemon/daemon.go`

1. Add `Pure bool` to the opencode provider config
   (`providers.opencode.pure`), default `false`.
2. Thread it to the serve-args builder (`http.go:287`) and append `--pure` when
   set.
3. Document in `docs/config.md` that the flag is fixed at engine spawn: because
   the engine is shared and prewarmed, changing `pure` requires an engine
   restart (daemon restart, or whatever the existing engine-recycle path is) to
   take effect. An operator who flips it and sees no change otherwise has no way
   to know why.

**Tests:**

- Serve args include `--pure` when set and are byte-identical to today when not.
- Config parse round-trip.
- Live (`live_opencode`): an engine boots with `--pure` and serves a session —
  proving the flag does not change the API surface the provider depends on.

**Acceptance:** `providers.opencode.pure: true` boots an engine that loads no
third-party plugins.

**Rollback:** remove the field.

---

## Phase 3 — P2: explicit loopback `Origin` on the goose ACP dial

**Files:** `internal/provider/acphttp/conn.go`

1. Add `Origin` to the dial headers (`conn.go:147-151`), set to
   `http://127.0.0.1:<port>` derived from the same `baseURL` the WS URL is built
   from — so the two can never disagree.
2. Comment it with why: goose's `--allowed-origin` help states the default is
   "the default loopback origins", and mcremote currently relies on a *missing*
   `Origin` being tolerated. Sending the matching loopback origin satisfies the
   policy explicitly (MADR 0037 §2.3).

**Tests:**

- Unit: the dial header carries an `Origin` matching the base URL's host:port
  for several base URLs (guards the derivation, which is the only way this can
  be wrong).
- Live (`live_goose`): a session completes a prompt over the ACP WebSocket —
  the actual proof, since a rejected handshake fails the dial.

**Acceptance:** goose sessions still work, now with an explicit origin.

**Risk and rollback:** this is the one phase that can *cause* a failure — a
wrong explicit `Origin` is worse than none, because goose's default policy would
then have something concrete to reject. The live test is the gate; if it fails,
revert to sending no `Origin` (one-line removal) rather than guessing at
another value.

---

## Phase 4 — P2: correct the orphan-reaping comment

**Files:** `internal/provider/httpagent/provider.go`

1. Delete the `ReapOrphans` clause at `:348` — the function does not exist
   anywhere in the module.
2. Replace it with what the code actually guarantees: `SetProcessGroup` puts the
   engine in its own group; `SetDeathSignal` (Linux `Pdeathsig`) SIGKILLs the
   engine if the daemon dies; `TerminateProcessGroup` sends SIGTERM then SIGKILL
   to the whole group on the supervised path.
3. State the residual gap plainly: a SIGKILLed daemon triggers the engine's
   `Pdeathsig`, but a grandchild that called `setsid` has left the group and
   survives. Note that `procutil.FindByEnv` / `OwnerAlive` exist for a future
   reaper, so the follow-on is discoverable.

**Tests:** none — this is a comment. The claim is verified by the absent
declaration.

**Acceptance:** no comment in the tree asserts a reaper that does not exist.

**Follow-on (not this plan):** an actual startup reaper — scan for processes
carrying `EnvEngineOwner`, kill those whose owner token is dead. Needs a policy
for what is safe to kill and its own tests.

---

## Phase 5 — documentation and verification

1. `docs/config.md`: document `providers.grok.reasoning_effort` (engine-level,
   passed through to `grok agent`, no value validation — grok rejects unknown
   values) and `providers.opencode.pure` (opt-in, fixed at engine spawn).
2. Set MADR 0037 to Accepted with an implementation record, including anything
   phase 0 found that contradicts the probed contracts.
3. `make preflight`, `make race`, `make test-all`, `flutter test`.
4. Run the live-tagged tests explicitly — they are acceptance tests, not part of
   the normal loop (AGENTS.md).

---

## Delivery order

| # | Phase | Priority | Effort | Notes |
|---|---|---|---|---|
| 0 | Pin CLI contracts | P1 | 1 h | Gates 1-3 |
| 1 | grok `reasoning_effort` | P1 | 1-2 h | `ModelArgs` composition is the substance |
| 2 | opencode `pure` | P1 | 1 h | — |
| 3 | goose `Origin` | P2 | 30 min | Only phase that can cause a regression |
| 4 | Comment correction | P2 | 15 min | — |
| 5 | Docs and verification | P1 | 1 h | — |

Roughly 4-6 h.

## Commit boundaries

One commit per phase. Phase 3 stays separate from everything else: it touches
shared `acphttp` transport used by goose, and it is the one change whose failure
mode is "no sessions start", so it must be revertible alone.

## Definition of done

- `providers.grok.reasoning_effort` reaches the process and survives a
  per-session model override, with a test that fails if `ModelArgs` stops
  threading it.
- `providers.opencode.pure` boots a plugin-free engine.
- The goose ACP dial sends a matching loopback `Origin` and sessions still work.
- No comment claims `ReapOrphans`.
- Both new config keys documented in `docs/config.md`.
- Live tests pin `--reasoning-effort`, `--pure` and `--allowed-origin` on the
  probed versions.
- `make preflight`, `make race`, `make test-all`, `flutter test` green.
