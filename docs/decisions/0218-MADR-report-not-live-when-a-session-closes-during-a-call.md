---
status: accepted
date: 2026-10-30
decision-makers: Project Owner
consulted: "internal/session/manager.go and manager_lifecycle_test.go at 533a6055; internal/ws/server.go's writeSessionErr; apps/mobile/lib/data/ws/mc_exception.dart; ci-flakes.tsv; the Windows test host's make ci-windows log of 2026-10-30"
informed: anyone who adds a Manager operation that calls a provider session
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Report a session as not live when it closes during a call

## Context and Problem Statement

On 2026-10-30, `make ci-windows` on the Windows test host failed at A6 (`go test ./...`), while
gating `0217-PLAN-update-every-pin-to-its-current-release.md`'s P1:

```text
--- FAIL: TestPromptFailsAfterDisconnectAutoClose (0.00s)
    manager_lifecycle_test.go:176: want ErrNotLive, got session closed
```

P1 changes no Go file. The same test failed in Windows CI on 2026-10-24 (`ci-flakes.tsv:21`, run
`40113049697`, commit `7d2a3ecf`) and passed on rerun. The owner chose to fix it at its root, in a
pair of its own, before P1 lands: "Own pair, fix first (Recommended)".

### What the code does [read at `533a6055`]

**F1. Every Manager operation that reaches a provider takes the session, then calls it.**
`liveSession` (`internal/session/manager.go:2657-2665`) returns `e.sess` under `m.mu.RLock`, and
releases the lock. The caller then calls the provider without it. There are 19 callers:

* `handleUnsandboxedBang` `:1000`;
* `Prompt` `:2712`;
* `setMode` `:2774`;
* `SetExecutionEnvironment` `:2799`;
* `setCollaborationMode` `:2811`;
* `SetConfigOption` `:2828`;
* `Diagnostics` `:2889`;
* `ModelCatalog` `:2913`;
* `Cancel` `:2929`;
* `RespondPermission` `:2941`;
* `RespondQuestion` `:3168`;
* `DeclineQuestion` `:3186`;
* `Revert` `:3273`;
* `Unrevert` `:3293`;
* `Diff` `:3314`;
* `workspaceSession` `:3339`;
* `Shell` `:3355`;
* `shareSession` `:3371`;
* `RefreshSkills` `:3414`.

**F2. A close removes the entry first, then closes the provider.** `closeMatching` (`:3476-3565`)
deletes the entry under `m.mu` (`:3494`), unlocks, and only then calls `e.sess.Close` (`:3515`).
The order is deliberate: `Close` can block on provider teardown and history I/O, so it must not run
under the manager mutex. The comment in `manager_lifecycle_test.go:184-189` says the same. So any
provider call that fails *because* the session was closed fails after the entry is gone.

**F3. A disconnect marks the entry dead before the auto-close removes it.** The event pump sets
`e.dead = true` and `e.meta.Live = false` when it sees `StatusDisconnected` (`:1484-1487`). It
then calls `autoClose` (`:1899-1919`), which calls `closeMatching`. `liveSession` already treats a
dead entry as not live (`:2661`).

**F4. Providers report a closed session with a plain error.** For example:

* acpagent: `fmt.Errorf("session closed")`, at `session.go:342`, `:374`, `:906`, `:1074`,
  `:1169`;
* codex: `session.go:684`;
* httpagent: `session.go:461`, `:969`, `:1020`;
* the test's tracking fake: `manager_lifecycle_test.go:35`.

None is typed, so `errors.Is` cannot recognise one.

**F5. The window.** A call that takes the session before F2's delete, and reaches the provider after
F2's `Close`, returns the provider's plain error. The manager's contract for a session that is not
live is `ErrNotLive` (`:96-97`). The failing run's log shows that order: "session closed" (`:3552`)
is logged before "session auto-closed" (`:1915`). The test's loop (`:171-176`) prompts as fast as
it can, so it lands in the window now and then. Windows' scheduler makes that more likely.

**F6. What the phone sees.**

* `writeSessionErr` (`internal/ws/server.go:2134-2164`) maps `ErrNotLive` to `session_not_live`.
  Anything else gets the operation's fallback code, such as `session_prompt_failed` (`:2042`).
* The phone turns `session_not_live` into "That session is no longer running on the host — start
  again from Sessions." (`apps/mobile/lib/data/ws/mc_exception.dart:38-39`). For any other code it
  shows the raw message (`:77`).
* So in the window, a user who sends a prompt as the agent dies sees "session closed", not the
  actionable sentence.

## Decision Drivers

* **Fix the cause, not the test.** The race is in the product. Rerunning the test, or relaxing its
  assertion, would hide it.
* **One rule for every operation.** All 19 callers share F1's shape. Fixing `Prompt` alone leaves 18
  operations with the same window.
* **Keep F2's ordering.** `Close` must stay outside `m.mu`.
* **Do not slow the common path.** A successful call must not take a lock it did not take before.
* **Keep the provider's error.** The daemon log, and `errors.Is` against the provider's own
  sentinels, still need it.
* **Deterministic proof.** The test that shows the fix must fail on the old code every time, not
  sometimes.

## Considered Options

* **A — Re-check the entry when a provider call fails, in every caller** (proposed).
* **B — The same re-check, in `Prompt` only.**
* **C — Count in-flight calls per entry, and make `closeMatching` wait for them before `Close`.**
* **D — A typed `provider.ErrClosed` that every provider returns, and the manager maps.**
* **E — Rerun or loosen the flaky test.**

## Decision Outcome

Chosen option: **"A — Re-check the entry when a provider call fails, in every caller"**, because it
closes F5's window for all 19 operations, in one place, using only F2's existing ordering. It costs
nothing on success and keeps the provider's error.

### The decisions (accepted 2026-10-30: "accept the 0218 madr, approved to proceed with the plan")

* **D1 — `providerErr`.** It is a new unexported `Manager` method:

  ```go
  // providerErr is err, unless the session the call went to is no longer id's
  // live session: closed and removed (closeMatching deletes before it closes),
  // replaced, or marked dead by a disconnect. Then the call failed because the
  // session ended, and it is reported as ErrNotLive, wrapping err.
  func (m *Manager) providerErr(id string, sess provider.Session, err error) error
  ```

  * A nil `err` is returned as nil.
  * Otherwise it reads the entry under `m.mu.RLock`. When the entry is missing, is `dead`, or holds
    a different session, it returns `fmt.Errorf("%w: %q: %w", ErrNotLive, id, err)`. Both
    `errors.Is(err, ErrNotLive)` and `errors.Is(err, <the provider's error>)` then hold.
  * When the entry still holds `sess` and is live, `err` is returned unchanged. A provider error on
    a live session, such as `provider.ErrTurnBusy`, keeps its own mapping (`server.go:2150`).
* **D2 — Every caller in F1 routes the error of each provider call through `providerErr`.** That is
  the call made on the session `liveSession` returned, or on an interface asserted from it. Errors
  the manager produces itself are not routed: an unsupported capability (`!ok` after an
  assertion), or a validation failure. Those are not about the session ending.
* **D3 — F2's ordering is kept, and named as the invariant D1 relies on.** The comment at
  `closeMatching`'s delete says that `providerErr` depends on the entry being gone before `Close`
  runs.
* **D4 — The tests:**
  * **an in-package test of `providerErr`**, with one case each for: the entry removed, replaced,
    dead, still live, and a nil error. Each `ErrNotLive` case must also still match the provider's
    error with `errors.Is`;
  * **an end-to-end test of `Prompt`, deterministic.** A fake session's `Prompt` signals that it
    has been entered, then waits. The test emits a disconnect, waits until the fake's `Close` has
    run (F2 has deleted by then), and releases `Prompt`, which returns the fake's "session closed".
    `mgr.Prompt` must return `ErrNotLive`. On the old code it returns "session closed" every time;
  * **the same for `Cancel`,** the other operation every provider implements;
  * `TestPromptFailsAfterDisconnectAutoClose` is left as it is, and must pass.
* **D5 — Out of scope:**
  * the narrower window where a provider fails because its process died, before the pump has
    processed the disconnect. The manager does not yet know the session ended, so the provider's
    error is reported. D's typed error would close that one, and it would be a record of its own;
  * the operations that do not go through `liveSession`.

### Consequences

* Good, because a call that loses the race now answers `session_not_live`, and the phone shows its
  actionable sentence (F6).
* Good, because `TestPromptFailsAfterDisconnectAutoClose` stops failing on Windows, by a fix and not
  by a retry.
* Good, because a successful call takes no new lock. A failed call takes one read lock more.
* Neutral, because each of the 19 callers gains one wrapped return.
* Bad, because a future caller that forgets D2 reopens the window for that operation. D3's comment
  and `providerErr`'s doc comment are the reminder, and no test catches the omission.
* Bad, because D5's process-death window remains.

### Confirmation

* D4's new tests are seen failing first on a scratch copy without the fix. The `Prompt` and
  `Cancel` cases fail every time, with the provider's message.
* With the fix, `go test -race ./internal/session/...` passes on this Mac and on the Linux test
  host. `make ci-windows` passes on the Windows test host, A6 included.
* On the Windows test host, `go test -run '^TestPromptFailsAfterDisconnectAutoClose$' -count=300
  ./internal/session/` is run on a scratch copy without the fix, and then with it. The failure
  count before is recorded as found, and it must be 0 after.

## Pros and Cons of the Options

### A — Re-check on failure, every caller (proposed)

* Good, because one helper covers all 19 operations, with F2's ordering as its whole basis.
* Good, because nothing changes on success.
* Bad, because the rule is a convention each new caller must follow (D3).

### B — `Prompt` only

* Good, because it is the smallest change that fixes the failing test.
* Bad, because the other 18 operations keep the same window. Cancelling as the agent dies is as
  likely as prompting.

### C — Wait for in-flight calls before `Close`

* Good, because no call could reach a closed provider at all.
* Bad, because `closeMatching` would wait on long provider calls: `Diff`, `Shell`, a `Prompt` that
  blocks on a full queue. A disconnect's cleanup would then be slow.
* Bad, because it adds a per-entry counter and a wait to every call, the successful ones included.

### D — A typed `provider.ErrClosed`

* Good, because the manager could recognise a closed provider directly, D5's process-death window
  included.
* Bad, because it touches every provider (F4) and every fake, which is a much larger change than
  the race needs. It can follow on its own.

### E — Rerun or loosen the test

* Bad, because the product race stays, and the phone keeps showing "session closed".
* Bad, because it is the workaround the owner's rules exclude.

## More Information

### Related records

* `0217-PLAN-update-every-pin-to-its-current-release.md`, Deviation 3: where this was found. Its P1
  waits for this pair.
* [0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md):
  its ledger, `ci-flakes.tsv`, has the earlier Windows CI failure of the same test at row 21.
