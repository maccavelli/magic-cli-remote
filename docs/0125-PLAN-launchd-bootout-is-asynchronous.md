---
status: in-progress
date: 2026-08-29
associated-madr: "0125-MADR-launchd-bootout-is-asynchronous.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0125 — Wait for launchd teardown; never leave the service stopped

Implements [0125-MADR-launchd-bootout-is-asynchronous.md](0125-MADR-launchd-bootout-is-asynchronous.md)
decisions D1–D7, closing findings F1–F7.

## Goal

`update` and `setup-service --force` succeed on the first attempt on a real
Mac, and a failure of either leaves the service **running** or says plainly
that it could not be restarted.

Finish line:

* one helper waits for the job to leave the domain; no site sleeps a constant;
* `setup-service --force` twice in a row succeeds twice;
* `update` over a running service succeeds first time;
* an induced start failure ends with the service running and a message that
  distinguishes "rolled back and running" from "rolled back and stopped";
* the owner has run all four on macOS and said so.

## Scope

### In scope (the only files any phase may touch)

* `internal/cli/service/control.go` — the wait helper, `Stop`, `Start`
* `internal/cli/service/setup.go` — the two discarded `bootout` calls
  (`:455`, `:634`) and the bootstrap sequence
* `internal/update/swap.go` — the 300 ms sleep and the rollback restart
* `internal/cli/service/control_test.go`, `setup_launchd_test.go`,
  `internal/update/*_test.go` — sequencing tests through the existing seams

### Out of scope

* **The Linux and Windows service paths.** `systemctl stop` is synchronous and
  the Windows path has its own control flow. MADR open question 4 asks whether
  Linux shares the shape; **investigate, do not change** — if it does, that is
  its own record.
* **The plist contents, labels, domains, or hardening.** This record is about
  *when* commands run, not what they install (0069 owns that).
* **Retry/backoff around bootstrap** (D6). Explicitly rejected; adding it would
  mask a wait that is wrong.
* **`launchdLastExitNote` and the diagnostics prose.** Adjacent, untouched.

## Stability rule

Every phase ends with:

```bash
go build ./... && go vet ./...
go test ./internal/cli/service/ ./internal/update/
```

then **one commit** (`git commit --no-edit`; never `-m`).

**Local `gofmt -l` is unusable on this host** (stale CRLF checkout). CI's Gofmt
step is the authority; do not run `make fmt`.

**Unit tests here prove sequencing, not timing.** They drive
`OverrideRunLaunchctl` and never touch launchd. A green suite means the commands
are issued in the right order with the right waits *modelled* — it does not mean
the race is fixed. Only P4 can say that.

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — No sleep stands in for a condition.** D2. A `time.Sleep` introduced to
"settle" anything is this bug being reintroduced. The 300 ms in `swap.go:99` is
removed, not tuned.

**C2 — A failure never leaves the service stopped silently.** D5. Every exit
path either has the service running or carries a message saying it does not and
why. This is the contract the user actually reported.

**C3 — No new retry around bootstrap.** D6. If a wait is correct, a retry adds
nothing; if it is wrong, a retry hides it.

**C4 — Linux and Windows behaviour is unchanged.** The helper is darwin-only.
A diff in the systemd or schtasks path is out of scope.

**C2 is the one at risk.** The natural fix is to make the *happy* path wait
correctly and stop there, because that is what reproduces. The reported harm —
a stopped service after a failed update — lives entirely in the rollback, which
only runs when something has already gone wrong and is therefore the path least
likely to be exercised while fixing this.

## Dependency and delivery order

P1 (helper + wait) → P2 (setup path) → P3 (update path and rollback) are
ordered so each lands with the helper it needs. P4 is owner verification on
macOS and can only follow all three.

## Implementation Steps

### P1 — A wait-for-teardown helper (D1, D2, D3; closes F1, F5)

`control.go`. Add a darwin helper that boots the job out and waits for it to
actually leave the domain: poll `launchctl print` until it reports absent, or
`exited`/`not running`, with a deadline; return a distinguishable timeout error
naming the label and the elapsed wait.

D3 lands here: `bootout`'s error stops being discarded. "Not loaded" is the
expected case and must be told apart from a real failure — MADR open question 2
must be answered while writing this, because the helper's correctness depends
on whether a non-zero `print` means "gone" or "wrong label".

Answer open question 1 in the commit: pick a deadline and say why. The 15 s
active-wait already in `swap.go` is the precedent to match or deliberately
differ from.

**Verification:** `control_test.go`, through `OverrideRunLaunchctl` and
`runLaunchctlCapture`, driving a scripted `print` that reports the job present
twice and then absent — the helper must poll, not sleep, and must return only
once the job is gone. A second case pins the timeout path.

### P2 — `setup-service --force` waits (D1, D3, D4; closes F3)

`setup.go`. Replace `_ = runLaunchctl("bootout", svc)` at `:455` with the P1
helper, and surface its failure instead of proceeding into a bootstrap that
cannot work.

D4 lands here too: when the plist is byte-identical to what is wanted and the
job is loaded, `kickstart -k` in place and skip the bootout/bootstrap cycle
entirely. `res.Unchanged` is already computed at `:426`; this is using a fact
the function already has.

`:634` (remove) gets the helper for consistency, though it is benign — the
plist is deleted immediately after.

**Verification:** extend `setup_launchd_test.go`. `TestDarwinSetupAgentOrder`
already asserts command order; add a case proving no `bootstrap` is issued when
the plist is unchanged and the job is loaded, and one proving the sequence waits
before bootstrapping when it is not.

### P3 — The update path and the rollback (D1, D5; closes F2, F4)

`swap.go`. Delete the 300 ms sleep (C1) and use the P1 helper for the stop.

Then the part that matters (C2): the deferred rollback at `:122-126` must wait
the same way before restarting, and its failure must reach the caller. Today it
logs and the process exits as though the rollback succeeded. After this phase
the outcome is one of exactly two, and they are distinguishable to the user:

```text
rolled back, service running      -> the good failure
rolled back, service NOT running  -> said loudly, with what to run
```

**Verification:** an update test that forces the post-swap `Start` to fail and
asserts (a) the binary is restored, (b) `Start` is retried after the wait, and
(c) when it still fails, the error surfaces rather than being logged. That last
assertion is C2 and is the one this plan exists for.

### P4 — Owner verification on macOS (D7; closes F7)

**Cannot be done from the development host.** The four rows of the MADR's
Confirmation block, run on the owner's Mac:

```text
setup-service --force, twice in a row   -> both succeed, service running
update over a running service           -> succeeds first time, service running
update with a forced start failure      -> service RUNNING, message says rolled back
launchctl print after each              -> state = running
```

Record the actual output in this plan's execution record, not a summary. If any
row fails, this plan returns to P1 rather than adding a retry (C3).

## Verification (whole plan)

```bash
go build ./... && go vet ./...
go test ./internal/cli/service/ ./internal/update/
grep -rn 'time.Sleep' internal/update/swap.go internal/cli/service/   # no settle sleeps
```

```text
unit tests            -> sequencing and waits, on any host
owner macOS run       -> the four rows above
```

### Acceptance criteria

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | One helper waits for teardown; every darwin teardown uses it | D1 |
| A2 | No `time.Sleep` stands in for a launchd state change | D2, C1 |
| A3 | `bootout` failures are classified, not discarded | D3 |
| A4 | Unchanged plist + loaded job restarts in place, no bootout | D4 |
| A5 | A failed op leaves the service running, or says it does not | D5, C2 |
| A6 | No retry/backoff added around bootstrap | D6, C3 |
| A7 | Linux and Windows paths unchanged | C4 |
| A8 | Owner has run the four macOS rows and the output is recorded | D7, F7 |
| A9 | MADR open questions 1–3 answered in the record | — |

**A5 is the one to guard**, for the reason under C2. **A8 is the one that
cannot be faked**: unit tests here prove ordering through a fake launchctl and
nothing more. A plan that reports "fixed" on a green `go test` has claimed
something it did not check.

## Rollout and Rollback

macOS only; Linux and Windows untouched. No persisted state, no protocol
change. Each phase reverts independently. The user-visible effect is that
`update` and `setup-service --force` stop failing intermittently — and, when
they do fail, stop leaving the daemon down.

## Deferred (named, so they are not mistaken for oversights)

* **Whether the Linux path shares the shape** (MADR open question 4).
  `Stop` calls `systemctl --user stop` without `--wait`, and the same rollback
  code is shared. Investigated in P3 by reading, not changed; if it is a real
  defect it gets its own record rather than riding along on a macOS fix.
* **Removing `Start`'s bootstrap→kickstart fallback** (MADR open question 3,
  F6). Once the wait is correct that fallback is dead weight, and leaving it
  invites the next reader to conclude the race is still live. Removing it is a
  behaviour change on a path this record is already touching, so it wants its
  own decision rather than being folded in quietly.
* **A macOS CI lane.** Would have caught none of this — the failure is timing
  under real launchd, and 0120 D11 declined the lane on value. Named so it is
  not proposed as the fix.
