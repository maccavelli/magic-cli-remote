---
status: accepted
date: 2026-08-28
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Make goose file-log tail attach observable so the Windows quota test is deterministic

## Context and Problem Statement

CI run [33223300684](https://github.com/maccavelli/magic-cli-remote/actions/runs/33223300684)
(`f8a9237`, 2026-08-29) failed the `Go (windows/amd64)` lane on a single test:

```text
--- FAIL: TestTailGooseFileLogsSurfacesQuota (3.29s)
    engine_log_tail_test.go:110: timed out waiting for error
```

Ubuntu Go, Flutter, and `Go (linux/arm64)` passed on that run. The commit is
docs-only (0120's execution record). Nothing in 0120, 0119, or 0118 touches
`internal/provider/acphttp`. This is a distinct failure from the arm64
codex races tracked by
[0119](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md).

The question is how to make `TestTailGooseFileLogsSurfacesQuota` prove the
0073 file-tail contract without a sleep standing in for "the tailer has
attached and seeked to EOF".

### What was measured, not assumed

**The assertion is real, and the quota line was classified — too late.**
`recvType` waits two seconds (`session_test.go:160`). The test slept 350 ms
for a 250 ms ticker, then appended the goose 429 line, then waited for
`event.TypeError`. The job log then shows:

```text
INFO tailing goose engine file logs dir=C:\Users\RUNNER~1\AppData\Local\Temp\TestTailGooseFileLogsSurfacesQuota…
--- FAIL: TestTailGooseFileLogsSurfacesQuota (3.29s)
    engine_log_tail_test.go:110: timed out waiting for error
WARN engine provider limit detected kind=quota text="Weekly usage limit reached. Resets in 4 days. GoUsageLimitError"
```

The production classifier fired. The test had already given up. This is not a
product miss of 0073 F1; it is a test that cannot see attach.

**The tailer has no attach signal.** `tailGooseFileLogs`
(`engine_log_tail.go:54-142`) polls on a 250 ms ticker. On the first
discovery of a path it stats the file, records `offset = fi.Size()`, and
`continue`s without reading — that is the seek-to-EOF that protects the
stale-line contract. Subsequent ticks read `[offset, size)`. The test
approximates "first tick has happened" with `time.Sleep(350 * time.Millisecond)`
(`engine_log_tail_test.go:95-96`).

**Two races share that sleep, and either explains this log.**

* **R1 — attach after the write.** Ticker first fire is specified as "after
  the duration", not immediately (`time.NewTicker`). If the first tick lands
  after the 350 ms sleep, attach seeks to EOF *including* the quota line and
  the test waits for an event that will never come. The later `kind=quota`
  log would then need another path (engine restart during teardown
  re-processing, or a later tick after truncation). The teardown log
  (`engine websocket lost; restarting engine err="read: connection reset"`)
  is consistent with the blocked `session/prompt` being cancelled after
  `t.Fatalf`.
* **R2 — attach on time, read late.** First tick attaches before the write;
  a later tick should see `size > offset`. Windows `Stat` size visibility
  after a same-process append is not instantaneous under load. A 2 s
  `recvType` window is then a timing bet. The `kind=quota` line in the same
  job, with an slog timestamp inside the fail window, fits this reading
  without needing a restart.

Both are the same design error: the test guesses that attach has happened.
It does not observe it. R1 vs R2 is not settled by one log, and does not
need to be: both close if attach is a signal rather than a sleep.

**The test is the only coverage of the file tailer.**
`grep -n 'tailGooseFileLogs\|gooseTail' internal/provider/acphttp/` hits
the production function, `startEngineFileLogTail`, and this one test.
Lengthening the sleep, skipping on Windows, or dropping the test would
remove the only pin of 0073's "quota lives in `~/.local/state/goose/logs/cli`,
not stderr" contract from CI.

**This host does not reproduce it.** `go test -count=20
-run TestTailGooseFileLogsSurfacesQuota ./internal/provider/acphttp/` is
the verification this record will run; it is not evidence the Windows
window is closed. 0119 made the same distinction and was right.

**windows/amd64 was green on the five completed runs immediately before
#388.** Runs 33101607297, 33210742346, 33219191191, 33221074719,
33221972891. One red in six is a flake, not a new product regression.

### Findings

**F1 — `Go (windows/amd64)` is red on the latest completed `master` run
because this test timed out waiting for a TypeError the tailer later
emitted.** Run 33223300684, job `Go (windows/amd64)`,
`engine_log_tail_test.go:110`.

**F2 — The 350 ms sleep is not an attach barrier.** The ticker period is
250 ms and first-fires after one period. A delayed first tick, or a delayed
size update after append, both beat the sleep. This is the same class of
defect 0119 D4 forbids: a sleep standing in for a happens-before.

**F3 — The production tailer is unobservable.** After seek-to-EOF there is
a debug log (`following goose log file`) and nothing a test can wait on.
`Provider` already has test seams of this shape (`probeFn`, and in codex
`doctorRun`); the tailer has none.

**F4 — Skipping, gating, or lengthening the wait would hide 0073's only
automated check.** 0119 D5 / 0118 D2 applied here: a test that cannot run
must say so for a stated reason, not disappear, and a longer sleep is not
a reason.

**F5 — 0119 does not own this.** Different package, different OS, different
mechanism, docs-only commit. Folding it into 0119 would mix a codex
singleflight repair with an acphttp attach seam.

## Decision Drivers

* A file-tail test must observe attach, then write, then assert. Guessing
  via `Sleep` is how this went red.
* The 0073 contract stays tested on every lane that runs `go test ./...`.
* Test seams match existing `Provider` style: nil in production, set by
  tests, no behaviour change when unset.
* 0119's C2 applies: no skip, no `testing.Short()`, no retry wrapper, no
  loosened assertion, no longer sleep as the fix.

## Considered Options

* **A — Observe attach, then write.** Add a nil test seam fired after the
  tailer records `offset = fi.Size()` on a newly discovered path. The test
  waits on that signal (timeout = failure), then appends, then keeps the
  existing `TypeError` / `quota` assertions. `f.Sync()` after the append
  so the next poll cannot lose the write to Windows size lag.
* **B — Lengthen the sleep and `recvType` deadline until Windows is lucky.**
* **C — Skip the test on Windows.**
* **D — Do nothing; treat one red as noise.**

## Decision Outcome

**Chosen: A — observe attach, then write.**

F2 is the defect, F3 is why the test had to guess, and A is the happens-before
the sleep was impersonating. B is 0119 C2 in another file. C deletes the only
coverage of 0073's file-tail path on the lane that just caught it. D leaves
`master` with a second independent flake on top of 0119.

R1 vs R2 is recorded as unresolved and does not change the repair: both
races die if the write is sequenced after attach.

### The decisions

**D1 — `tailGooseFileLogs` fires an optional `Provider` seam after it
seeks to EOF on a newly discovered log.** Nil in production. The call
site is the existing `path != curPath` branch, after `offset = fi.Size()`,
before `continue`. That is attach. A hook on every poll, or on every
read, is the wrong event.

**D2 — `TestTailGooseFileLogsSurfacesQuota` waits for that seam, then
appends, then asserts.** The 350 ms sleep is deleted. `recvType` for
`TypeError` and the `ErrorKind == "quota"` / natural-language checks stay.
A timeout waiting for attach is a failed test, not a skip.

**D3 — The test `Sync`s the appended line before waiting for the event.**
This is not a barrier for attach — attach is D2. It is so the next 250 ms
poll cannot observe a stale size on Windows. Same-process `Write` +
`Close` is not a documented happens-before for `Stat` on that runner.

**D4 — No skip, no GOOS gate, no longer sleep, no retry.** F4.

**D5 — Production behaviour with the seam unset is unchanged.** The field
is a func, zero value nil, one nil-check. No ticker change, no fsnotify,
no poll-interval injection.

**D6 — This record does not wait on 0119 and does not change `.github/workflows/ci.yml`.**
Windows observation of the fix is a push-gated phase of the plan, same
rule as 0119 P4.

### Consequences

* Good, because the only 0073 file-tail test becomes a happens-before
  rather than a timing bet, on every OS the suite runs.
* Good, because the seam is the same shape as existing `Provider` test
  hooks and is inert when nil.
* Bad, because a production file (`provider.go`, `engine_log_tail.go`)
  gains a test-only field. Accepted: attach is inside the tailer; a test
  cannot observe it without a seam, and slog-scraping the debug line is
  a worse coupling.
* Neutral, because R1 vs R2 stays unresolved. The repair does not depend
  on the distinction.

### Confirmation

```bash
gofmt -l internal/provider/acphttp/provider.go \
         internal/provider/acphttp/engine_log_tail.go \
         internal/provider/acphttp/engine_log_tail_test.go
go vet ./internal/provider/acphttp/
go test -count=50 -run TestTailGooseFileLogsSurfacesQuota ./internal/provider/acphttp/
go test -count=20 ./internal/provider/acphttp/
```

```text
engine_log_tail_test.go → no time.Sleep for attach
Go (windows/amd64)      → TestTailGooseFileLogsSurfacesQuota run, not skipped
                          (plan P2, needs a push)
```

## Pros and Cons of the Options

### A — Observe attach, then write (chosen)

* Good, because it is the barrier the sleep was faking.
* Good, because both R1 and R2 close.
* Bad, because it edits production files for a test-only field.

### B — Lengthen the sleep

* Good, because one line, no production edit.
* Bad, because F2: a longer guess is still a guess, and 0119 C2 forbids it.

### C — Skip on Windows

* Good, because the lane goes green.
* Bad, because F4: the lane that caught the flake stops checking the
  contract.

### D — Do nothing

* Good, because nothing.
* Bad, because `master` stays one flake away from red on a second lane.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Windows fail, timeout at `:110` | run 33223300684, `Go (windows/amd64)/6_Test.txt` |
| Quota classified after the fail | same log, `kind=quota` |
| 350 ms sleep, 250 ms ticker | `engine_log_tail_test.go:95-96`, `engine_log_tail.go:62` |
| Seek-to-EOF on first discover | `engine_log_tail.go:76-90` |
| `recvType` 2 s deadline | `session_test.go:158-172` |
| Only test of `tailGooseFileLogs` | grep over `internal/provider/acphttp/` |
| Prior five windows runs green | 33101607297, 33210742346, 33219191191, 33221074719, 33221972891 |
| Commit under the fail is docs-only | `git show f8a9237 --stat` |

### Related records

* [0073-MADR](0073-MADR-goose-prompt-hang-and-debug-pass.md) — F1 is the
  contract this test pins: goose writes 429/quota to the CLI log dir, not
  stderr.
* [0119-MADR](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md) — the
  other current CI redness; different package, different race. C2/D5 are
  reused here, not its file list.
* [0116-PLAN](0116-PLAN-windows-and-linux-arm64-build-targets.md) D17 —
  the windows lane exists to run tests, not to skip the ones that fail
  there.

### Open questions for the plan

1. ~~R1 or R2?~~ **Not decided, not needed.** Recorded above. Do not
   spend a phase reproducing Windows under a debugger to pick one.
2. Does `f.Sync()` belong in production `tailGooseFileLogs` writers? No.
   Goose owns those files. Sync is a test-side precaution on the append
   the test itself performs.
3. Should other poll-and-sleep tests in `acphttp` be swept? Out of scope;
   same deferral 0119 made for a package-wide race sweep.
