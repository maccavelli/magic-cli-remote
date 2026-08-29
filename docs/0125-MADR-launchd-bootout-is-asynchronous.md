---
status: accepted
date: 2026-08-29
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# Wait for launchd teardown instead of guessing at it, and restart on rollback

## Context and Problem Statement

Three launchctl-touching operations on a real Mac, two failures, two different
error codes:

```text
update (1st)            error 37  "operation already in progress"  — raced its own stop
update (2nd)            succeeded
setup-service --force   error 5   bootstrap over a live-or-half-torn-down job
```

Both failures left the service **stopped**, not rolled back and running.

Two error codes and an intermittent reproduction look like two bugs. They are
one fact the code never modelled — `launchctl bootout` returns before the job
is gone — papered over in four different places, four different ways.

### What was measured, not assumed

**`bootout` is asynchronous, and error 37 is what bootstrapping into an
unfinished teardown returns.** Error 37 is `EALREADY`: the job is still loaded.
The documented remedy is to ensure the job is fully unloaded before
bootstrapping again ([Homebrew/homebrew-core#132781](https://github.com/Homebrew/homebrew-core/issues/132781)).
Error 5 is `EIO`, which launchd returns for a bootstrap it cannot service —
including one over a job still in the domain
([Apple Developer Forums 748205](https://developer.apple.com/forums/thread/748205)).
The two codes are two points in the same teardown window, not two defects.

**Four call sites, four different mitigations, no shared model.**

| Site | Mitigation | Result |
| --- | --- | --- |
| `setup.go:455` — `setup-service --force` | `_ = runLaunchctl("bootout", svc)`, error discarded, **no wait at all** | bootstrap lands mid-teardown → **error 5** |
| `swap.go:99` — `update` | `sleep(300 * time.Millisecond)`, comment: *"Settle for ETXTBSY / launchd bootout async teardown"* | insufficient under load → **error 37** |
| `control.go:97-102` — `Start` | bootstrap fails → try `kickstart` once, comment: *"Race: another path may have bootstrapped"* | hides the race sometimes |
| `setup.go:634` — remove | `_ = runLaunchctl("bootout", svc)`, no wait | benign; the plist is deleted next |

The 300 ms in `swap.go` is the only place that acknowledges the asynchrony, and
it acknowledges it with a constant rather than a condition. Nothing anywhere
polls for the job to actually leave the domain.

**The rollback restarts with the same broken primitive, and swallows the
result.** `swap.go:122-126`:

```go
if stopped && wantUp && svc != nil {
    if serr := svc.Start(opts.Product); serr != nil {
        log("restart after failure: " + serr.Error())
    }
}
```

`Start` on darwin is `print` → if not loaded → `bootstrap` (`control.go:88-106`).
When the original failure *was* an unfinished teardown, the rollback fires
immediately into that same window, fails the same way, and the error is only
logged. That is the precise mechanism behind "left stopped, not rolled back and
running" — the rollback is not missing, it is racing.

**The building block already exists and is unused for this.** `IsActive`
(`control.go:12-40`) already parses `launchctl print` and distinguishes
`state = running` / `waiting` from `exited` / `not running` / absent. Nothing
calls it to *wait*; every site fires and hopes.

**The seam to test sequencing exists and is already used.**
`OverrideRunLaunchctl` (`setup.go:209`) and `runLaunchctlCapture`
(`control.go:240`) let a test drive the launchd path with no launchctl present.
`setup_launchd_test.go` and `control_test.go` already assert command *order* on
a non-Darwin host — `TestDarwinSetupAgentOrder`,
`TestStartDarwinBootstrapsWhenNotLoaded`.

**Nobody can reproduce this on the development host.** This project's active
machine is Windows; the owner has a Mac. Real launchd timing is observable only
there, which is why every mitigation so far is a guess with a plausible comment
attached.

### Findings

**F1 — One root cause, not two bugs.** `bootout` is asynchronous and no call
site waits for the job to leave the domain. Error 37 and error 5 are two
landing points in the same window.

**F2 — The 300 ms sleep is a guess, and it is the good case.** It is the only
site that acknowledges the asynchrony at all, and it still failed. A constant
cannot be right: teardown time depends on the job's shutdown, disk, and load.

**F3 — `setup-service --force` has no wait whatsoever.** The `bootout` error is
discarded and `bootstrap` follows immediately. That path is not unlucky; it is
unsynchronised by construction.

**F4 — The rollback races the same window and hides its own failure.** It calls
`Start` immediately after a failure that was itself caused by an unfinished
teardown, and logs rather than reports. The user sees a stopped service and a
success-shaped exit path.

**F5 — A wait condition is already computable.** `IsActive` reads exactly the
state a wait would need. The gap is a loop and a deadline, not new knowledge.

**F6 — `Start`'s bootstrap→kickstart fallback is a third mitigation of the same
fact.** It sometimes converts the race into success, which is why `update`
succeeded on the second run and why the bug looks intermittent.

**F7 — This cannot be verified on the development host.** Only the owner's Mac
can confirm the fix. Any plan that claims otherwise is claiming something it
cannot check.

## Decision Drivers

* A failed service operation must leave the service **running**, or say clearly
  that it could not. Silently stopped is the worst outcome and it is the one
  being shipped.
* One fact should be modelled once. Four mitigations for one asynchrony is how
  the two error codes ended up looking unrelated.
* Wait for a *condition*, never a duration. A sleep long enough for a slow Mac
  is a tax on every fast one and still not a guarantee.
* Do not paper further. A retry around a race makes the failure rarer and the
  diagnosis harder.

## Considered Options

* **A — Wait on the observable condition, in one helper, used by every site.**
* **B — Increase the sleep and add one to the setup path.**
* **C — Retry bootstrap on 37/5 with backoff.**
* **D — Stop using `bootout`; use `kickstart -k` alone to restart in place.**

## Decision Outcome

**Chosen: A, with D adopted where it applies.**

B is what is already there, and it already failed. Any constant is wrong on
some machine.

C treats the symptom. A retry loop around bootstrap would work most of the
time and would make the remaining failures harder to read, because the error
surfaced would be the last attempt rather than the condition. It also does
nothing for F4 — the rollback would still race.

D is genuinely better *where it applies*: a plist that has not changed does not
need a bootout/bootstrap cycle at all, and `kickstart -k` restarts a loaded job
in place with no teardown window to race. It does not cover the case that
requires a reload — a changed plist — so it narrows the problem rather than
replacing A.

### The decisions

**D1 — One helper waits for the job to leave the domain, and every teardown
goes through it.** Poll `launchctl print` until it reports the job absent (or
`exited`/`not running`), with a deadline. Replaces the 300 ms sleep, the
discarded `bootout` at `setup.go:455`, and the discarded one at `:634`.

**D2 — Wait on a condition with a deadline, never on a duration.** On timeout,
say what was waited for and for how long, and fail — a bootstrap into a job
that is provably still there will not succeed by being attempted.

**D3 — `bootout` failures stop being discarded.** `_ = runLaunchctl("bootout")`
throws away the one signal that says teardown will not happen. "Not loaded" is
the expected, ignorable case and must be distinguished from a real failure
rather than lumped with it.

**D4 — Restart in place when the plist is unchanged (option D).** If the
definition on disk is byte-identical to what is wanted and the job is loaded,
`kickstart -k` and do not bootout at all. This removes the window entirely for
the common `update` case, where only the binary changed.

**D5 — A failed operation must leave the service running or report that it
could not.** The rollback restart must use the same D1 wait, and its failure
must reach the user rather than a log line. "Rolled back and stopped" is a
distinct, worse outcome than "rolled back and running", and the exit status and
message must tell them apart.

**D6 — Do not add a retry around the race (option C rejected).** Once D1 waits
on the real condition, a retry only masks a wait that was wrong.

**D7 — The fix is verified on the owner's Mac, and the record says so (F7).**
Unit tests pin the *sequencing* through the existing override seam and run
anywhere; they cannot prove launchd timing. No claim of "fixed" is made on
unit tests alone.

### Consequences

* Good: two symptoms with different error codes collapse into one modelled
  fact, and a third latent mitigation (`Start`'s kickstart fallback) stops
  being load-bearing.
* Good: the common `update` case stops booting the job out at all (D4), so the
  window it raced no longer exists there.
* Good: a failed update leaves a running service, or says plainly that it did
  not.
* Bad: the fix cannot be proven on the machine where it is written (F7). Every
  claim here is either a unit-tested sequence or an owner-observed run, and the
  plan must not blur them.
* Neutral: Linux and Windows paths are untouched — `systemctl stop` is
  synchronous and the Windows path has its own control flow.

### Confirmation

```bash
go build ./... && go vet ./...
go test ./internal/cli/service/ ./internal/update/
```

```text
setup-service --force, twice in a row   -> both succeed, service running
update over a running service           -> succeeds first time, service running
update with a forced start failure      -> service RUNNING, message says rolled back
launchctl print after each              -> state = running
```

The four rows above are owner-run on macOS. Nothing in this record claims them
from a Windows host.

## Pros and Cons of the Options

### A — Wait on the observable condition (chosen)

* Good: models the fact once; every site inherits it.
* Good: fast machines stop paying a fixed tax, slow ones stop failing.
* Good: a timeout is a real diagnosis ("job still loaded after Ns"), not a
  guess.
* Bad: polling `launchctl print` in a loop is more code than a sleep.

### B — Longer sleep, and add one to setup

* Good: two-line change.
* Bad: it is the current design, and it failed. Any constant is wrong
  somewhere.

### C — Retry bootstrap on 37/5

* Good: would probably make the reported symptom go away.
* Bad: hides the condition; the reported error becomes the last attempt's.
* Bad: does nothing for the rollback (F4).

### D — Restart in place, never bootout (adopted where applicable)

* Good: removes the race window instead of surviving it.
* Good: exactly right for `update`, which changes the binary, not the plist.
* Bad: cannot serve a changed plist, which must be reloaded.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Reported codes and sequence | owner, macOS, 2026-08-29 |
| Error 37 = EALREADY, job still loaded | [homebrew-core#132781](https://github.com/Homebrew/homebrew-core/issues/132781) |
| Error 5 = EIO on bootstrap | [Apple Developer Forums 748205](https://developer.apple.com/forums/thread/748205) |
| No wait in `setup-service --force` | `internal/cli/service/setup.go:454-455` |
| 300 ms sleep in update | `internal/update/swap.go:98-100` |
| Bootstrap→kickstart race fallback | `internal/cli/service/control.go:97-102` |
| Discarded bootout on remove | `internal/cli/service/setup.go:634` |
| Rollback restart logs and swallows | `internal/update/swap.go:122-126` |
| `Start` bootstraps when not loaded | `internal/cli/service/control.go:88-106` |
| State is already parseable | `internal/cli/service/control.go:12-40` |
| Test seam exists and is used | `setup.go:209`, `control.go:240`, `setup_launchd_test.go:18` |

### Related records

* [0072](0072-MADR-phone-reconnect-and-provider-timeout-incident.md) — its P2
  established the print→kickstart/bootstrap order in `Start`, cited in
  `control.go:76`; that path's race fallback is F6.
* [0100](0100-MADR-update-unit-refresh-and-daemon-reload.md) and
  [0103](0103-MADR-update-tracks-release-build-and-active-service.md) —
  `HealStart` and the `installed` gating on the same restart path this record
  finds racing (`run.go:138-152`).
* [0069](0069-MADR-macos-permissions-and-sandbox-parity.md) — macOS LaunchAgent
  hardening; the domain and label conventions this record uses.

### Open questions for the plan

1. What deadline? Long enough for a loaded job to drain on a slow Mac, short
   enough that a genuinely stuck job is reported rather than waited on. The
   existing 15 s active-wait in `swap.go` is a precedent worth matching or
   deliberately differing from.
2. Does `launchctl print` distinguish "gone" from "never existed" usefully, or
   does the wait have to treat a non-zero exit as success? This determines
   whether the helper can tell teardown-complete from wrong-label.
3. Should `Start`'s bootstrap→kickstart fallback (F6) be removed once D1 lands,
   or kept as a genuine belt-and-braces? Keeping a mitigation for a fixed race
   is how the next reader concludes the race is still live.
4. Does the Linux path have the same shape? `systemctl --user stop` is
   synchronous by default, but `Stop` does not pass `--wait`, and the same
   rollback code is shared. Worth a look, not assumed.
