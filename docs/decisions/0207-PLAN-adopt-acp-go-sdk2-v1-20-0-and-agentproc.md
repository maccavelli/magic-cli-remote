---
status: in-progress
date: 2026-10-22
associated-madr: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the `acp-go-sdk2 v1.20.0` move and the adoption of `agentproc` and the connection hooks

Associated MADR: [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
its amendment of 2026-10-22.

## Goal

* Magic builds against `acp-go-sdk2 v1.20.0`.
* Every ACP agent process is started, tied to its connection, and stopped by the SDK's
  `agentproc`, with the wire capture kept.
* The phone is told why an agent went away, which message type was dropped, and when an agent sent
  something the SDK refused.

## Scope

* **In scope:** the MADR's D2 as amended, D10, D11 with the comment half of D8, D12 and D13.
* **Not in scope:**
  * D1, the `go-selfupdate-lib v1.17.2` pin. It is one library per change (the MADR's drivers),
    so it takes its own plan.
  * D3, D4, D5 and D8's `binaryWarning` half. They are service and SemVer work for a later 0207
    plan.
  * D14: items 3 and 4 need no code.
  * gobble's pin, which is gobble's (G6).
  * `terminal.go`'s use of `procutil.SuperviseStarted` and `KillProcessGroup`. Those are the
    terminals Magic runs for an agent, not the agent, and they stay.
* **The owner's answers, 2026-10-22:**
  * **Q5, "Separate phases (Recommended)":** the pin is S1, and the process change S3, so they
    bisect apart.
  * **Q6, "Kill at once (Recommended)":** teardown keeps today's semantics.
  * **Q2, "Once, at S6 (Recommended)":** one `make live-grok` run, after S1 to S5.
  * **Q1, "A second 0207 PLAN (Recommended)":** the rest of 0207 is a sibling plan, not this one.

## Implementation Steps

### Rules for every phase

* **The phase's files are named at its start.** A file outside that list is a deviation: stop,
  prompt and record.
* **Every new test is seen failing first,** on a scratch copy of the tree, never by dirtying it.
  The execution record says what the failure looked like.
* **Before staging:** `make pre-add-check FILES="…"` on every staged `.go` file, then `make test`
  and `make race`.
* **A phase ends with its execution record** in this plan, and is staged for the owner, who
  commits it.
* No live suite runs without the owner's word for that run.

### S1 — Pin `acp-go-sdk2 v1.20.0` and retire B3's mitigation (D2)

**Files:**

* `go.mod`, `go.sum`;
* `internal/provider/acpagent/session.go`;
* `internal/provider/acpagent/lifecycle_test.go`, the test's comment only. Added by S1
  deviation 1.
* `docs/decisions/0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md` and
  `docs/decisions/0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md` (notes only);
* this plan.

1. `go get github.com/maccavelli/acp-go-sdk2@v1.20.0`, then `go mod tidy`.
   * `go.mod` changes one line.
   * `go.sum` changes the SDK's two lines and nothing else. Any other change is a deviation.
2. In `submitPrompt`, delete the `ctx.Err()` check and its comment (`session.go:602-609`).
   * Keep `TestPromptSendsNothingWithAnEndedContext`.
   * Its comment is updated: the SDK sends nothing for an ended context from `v1.15.2`, on
     issue #7.
3. **0184's PLAN.** Its amendment "B3 for `v1.15.2` (proposed)" gets a dated note: superseded by
   0207 D2, never run.
4. **0184's MADR.** Its status note records that issue #7 is closed and its fix is pinned.
5. **Verify:**
   * `go build ./...`;
   * `go vet ./...` with `GOOS` set to `darwin`, `linux` and `windows`;
   * `make test`, `make race`;
   * `go test -count=1 -run '^TestPromptSendsNothingWithAnEndedContext$' -v ./internal/provider/acpagent/`.

   The fail-first half of that test is on record: the MADR's probe saw it fail on `v1.15.0` with
   the check removed. The amendment's probe passed the whole race suite on `v1.20.0`.

### S2 — A writer for `wirecap` (preparing D10)

**Files:** `internal/wirecap/wirecap.go`, `internal/wirecap/wirecap_test.go`; this plan.

1. Add `func (c *Capture) Tap() io.Writer`.
   * **Nil capture.** For a nil `*Capture` it returns an untyped nil, so `agentproc` sees no tap.
   * **Otherwise** it returns a writer that splits on `\n` and calls `Frame` per line, as
     `teeReader` does.
     * It copies what it buffers, since `OutputTap` "must not retain p" (`agentproc.go:56`).
     * It always reports `len(p), nil`, since `agentproc` ignores the error.
2. **Tests:**
   * **Nil.** `Tap()` on a nil capture is `== nil` as an interface value.
     * Plant: return `(*tapWriter)(nil)`. The test must fail.
   * **Same frames as `TeeReader`.** The same bytes, written in uneven chunks, produce the same
     `frames.jsonl` as `TeeReader` read. That includes a line split across writes and a trailing
     line with no newline, which is held and not written, as `teeReader` holds it.
     * Plant: drop the buffering. The split-line case must fail.
   * **No retention.** The writer must not keep the caller's slice: the test overwrites it after
     `Write` returns.
     * Plant: store `p` instead of copying. The test must fail.
3. `TeeReader` stays until S3 removes its last caller.

### S3 — Start ACP agents through `agentproc` (D10)

**Files:**

* `internal/provider/acpagent/acpagent.go`, `session.go`;
* `prewarm_test.go`, `session_test.go`;
* a new `internal/provider/acpagent/agentproc_test.go`;
* `internal/wirecap/wirecap.go`, `wirecap_test.go`, for the `TeeReader` removal;
* `internal/provider/acpagent/extensions_test.go`, and a new
  `internal/provider/acpagent/agentproc_unix_test.go`. Added by S3 deviation 1.
* this plan.

1. **`spawnAgent`** (`acpagent.go:455-627`).
   * **Keep:** `launch.Resolve`, `launch.Command`, `cmd.Dir` and the stderr `slogWriter`.
   * **Delete:** `StdinPipe`, `StdoutPipe`, `cmd.Start`, the `SuperviseStarted` block and
     `killTree`.
   * **Start** with `agentproc.Command{Cmd: cmd, Grace: agentGrace, ConnectionOptions:
     clientConnOptions(log), OutputTap: s.wire.Tap()}.Start(s)`. Then set `s.proc` to the process
     and `s.conn` to `proc.Conn()`.
   * **`agentGrace`** is a named constant of 2 s, the order of `closeTimeout`. Under `Kill` it
     bounds only the tap drain in `Wait`, and that is its comment.
   * **The order is kept.** `s` is fully built before `Start`, because the SDK reads before `Start`
     returns, as it did before `NewClientSideConnection` returned.
2. **Spawn's failure paths** (`:548-549`, `:563-564`, `:569-570`, `:581-582`) call
   `s.proc.Kill()`, which kills and reaps. `cmd.Wait` is gone.
3. **The exit watcher** (`:598-618`) waits on `s.proc.Wait()`.
   * It keeps its panic guard.
   * It sends the same message as today: S4 changes the text.
4. **`session`** (`session.go`).
   * **Fields.** `cmd`, `release` and `procExited` are replaced by `proc *agentproc.Process`.
   * **`killProcessTree` is deleted.**
   * **`markClosedAndKill`** (`:1192-1205`) starts `s.proc.Kill()` on a goroutine of its own, when
     `proc` is not nil. That keeps its contract: it does not wait for the reap, and the watcher's
     `Wait` reaps.
   * **`Close`** (`:1371-1380`) does the same after `session/close` and `conn.Close`, as Q6
     assumes.
   * **No PID is signalled after the reap.** `agentproc` sends nothing to an agent that has exited
     (`agentproc.go:424-435`), so the `procExited` reasoning in `Close`'s comments goes with the
     field.
5. **`claimWarm`** (`acpagent.go:749`) treats a spare as dead when `s.proc.Done()` is closed (a
   non-blocking receive) or `s.closed` is set.
   * `TestClaimWarmRejectsDeadSpare` builds its dead spare without `procExited`. It uses a
     session whose `proc` has exited: a helper that starts and waits for a process that exits at
     once, from `agentproc_test.go`.
   * A nil `proc` with `closed` set covers the case without a process.
6. **`wirecap`.** `TeeReader`, `teeReader` and their tests go: nothing calls them. A `git grep
   TeeReader` empty outside `docs/` is the check.
7. **`agentproc_test.go`.**
   * **The helper agent.** The test binary is its own agent: under an environment variable, its
     `TestMain` serves a minimal ACP agent over stdio with `acp.NewAgentSideConnection`, answering
     `initialize`. The variable's name is `MCREMOTE_TEST_ACP_AGENT`.
   * **Spawn.** `spawnAgent`, with `cfg.Bin` set to the test binary, initializes. Then:
     * on Unix, the agent's process group is its own PID;
     * with `MCREMOTE_WIRE_CAPTURE_DIR` set to a temp dir, `frames.jsonl` holds the `initialize`
       answer;
     * `Close` ends the process, so the `Wait` the watcher holds returns.
     * Plant: start with `cmd.SysProcAttr.Pgid` preset. The spawn must fail, which shows the test
       sees `agentproc`'s refusal.
   * **Agent exit.** Killing the helper from outside makes the session emit `disconnected` once.
     * ~~Plant: drop the watcher. The test must fail.~~ Plant: drop both watchers, the exit
       watcher and `watchConnClose`. The test must fail. (S3 deviation 2: either watcher alone
       reports the death.)
8. **Verify:**
   * `make test`, `make race`;
   * `go vet ./...` with `GOOS` set to `darwin`, `linux` and `windows`;
   * `git grep -n -E "SuperviseStarted|KillProcessGroup|StdinPipe|StdoutPipe" internal/provider/acpagent`
     lists only `terminal.go`.

### S4 — The phone is told why (D11, and D8's comment)

**Files:** `internal/provider/acpagent/acpagent.go`, `session.go`, `session_test.go`,
`agentproc_test.go`; this plan.

1. **`watchConnClose`.**
   * Its parameter's interface gains `Err() error`. The tests' `fakeConn` implements it.
   * Its message is `"<id> agent connection lost: <conn.Err()>"`.
   * Its comment is corrected: under `OverflowDropNewest` an overflow drops a notification and
     does not end the connection (MADR F6).
2. **The exit watcher's message** carries `Wait`'s error unchanged. Magic's own `Kill` then reads
   `agentproc: agent killed: signal: killed`; an agent that died by itself reads its exit status.
   * *Added by S4 deviation 1:* `watchConnClose`, once the connection has ended, waits up to
     500 ms for the process to exit. If it does, `watchConnClose` stays silent and the exit
     watcher reports. Otherwise it reports `conn.Err()`.
3. **Tests:**
   * **`fakeConn`.** Its `Err` returns `fmt.Errorf("%w: %w", acp.ErrPeerDisconnected, io.EOF)`, and
     the `disconnected` error text names `peer disconnected`.
     * Plant: drop the `Err` from the message.
   * **The helper agent.** Killed from outside, its error event does not contain `agent killed`.
     After `Close`, no error event is sent at all, since `signalDisconnected` is silent once
     `closed` is set.
     * Plant: emit on close. The test must fail.

### S5 — Drop handler and inbound validation (D12, D13)

**Files:**

* `internal/provider/acpagent/acpagent.go`, `session.go`;
* `droppednotice_test.go`, `stalledpump_test.go`;
* a new `internal/provider/acpagent/refusednotice_test.go`;
* `docs/decisions/0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md`
  (an amendment note on D17);
* this plan.

1. **`clientConnOptions`** (`acpagent.go:1047`) takes the session.
   * It adds `acp.WithNotificationDropHandler`, which stores the method in an `atomic.Pointer[string]`
     on the session.
   * It adds `acp.WithInboundValidation`. That hook, when `err != nil`, adds one to an
     `atomic.Uint64` and stores the method, then returns `err` unchanged.
   * Neither does I/O, takes a lock or calls the connection.
   * `stalledpump_test.go` builds its connections through the same function, as its comment
     requires (0167 D19).
   * **Attribution.** `spawnAgent` builds one connection for each `session` it creates
     (`acpagent.go:479-531`). So the session is the connection's owner, and a refusal counts
     against it (MADR D13).
2. **D18's notice** (`noticeDroppedUpdates`, `session.go:640-657`) appends the last dropped method,
   when one was recorded: "… and 3 were dropped (last: session/update) …".
3. **`noticeRefusedUpdates(atStart uint64)`** runs where `noticeDroppedUpdates` runs, on the turn
   goroutine before `TypeTurnComplete`. When the count rose, it emits one `TypeNotice`: "The agent
   sent N messages the protocol does not allow, which were dropped (last: <method>)."
4. **Tests:**
   * **`droppednotice_test.go`.** The notice names the method.
     * Plant: omit it.
   * **`refusednotice_test.go`.** Over `newWireSession`, the agent sends a `session/update` that
     decodes but fails the SDK's validation: a `tool_call` whose `locations` holds a relative `path`.
     The SDK checks that path (`ToolCallLocation.validate`, `types_gen.go:18010-18021`), and "a
     failure drops the whole notification" (the SDK's
     `0015-MADR-validate-absolute-protocol-paths.md`, line 77). The turn then ends
     with exactly one refused-message notice, before `turn_complete`, and a valid update in the same
     turn is still delivered.
     * Plants: return nil from the hook, which must accept the bad update and fail the test; and do
       not count, so the notice is missing.
5. **0167.** An amendment note on D17: the handler now exists and only records (0207 D12).

### S6 — Acceptance on the other hosts, and live

**No files** but this plan's execution record.

1. **The Windows test host.**
   * `make ci-windows`, from Git Bash.
   * An `acpagent` test run with `-run 'TestSpawn'`, which starts the helper agent in its job.
     It starts a grandchild, and `Close` must end both.
   * Recorded with the host redacted.
2. **The Linux test host.**
   * `make test`, `make race`.
   * A scratch probe, not committed, kills the test process with SIGKILL while the helper agent
     runs. The agent must be gone within a second: `agentproc`'s `Pdeathsig`.
3. **`make live-grok`,** on the owner's word for that run. It closes D2's gate and runs D10's path
   against a real engine.

## Verification

* **Per phase:** the steps' own checks, the fail-first plants each recorded, `make pre-add-check`,
  `make test` and `make race`.
* **Once the phases are done:**
  * `go.mod` names `acp-go-sdk2 v1.20.0`;
  * the `git grep` of S3 step 8;
  * `make check-records`;
  * markdownlint on the records changed.
* **The MADR's "Confirmation of the amendment"** is met item by item, with S6 the Windows and live
  items.

## Rollout and Rollback

* Each phase is one commit, so each reverts alone.
* **S1 reverted** restores `v1.15.0` and the mitigation together. They must not be split: without
  the check, `v1.15.0` sends a stray `session/cancel`.
* **S3 reverted** restores the hand-written process code. S4 and S5 come after it and revert first.
* **The daemon carries no state about this change.** A release with it, or rolled back, needs
  nothing from the operator.
* Nothing is pushed or tagged by this plan; the owner pushes.

## Execution record

### S1 — 2026-10-22

#### Deviation 1 — 2026-10-22: `lifecycle_test.go` added to S1's files

* **Found:** step 2 updates `TestPromptSendsNothingWithAnEndedContext`'s comment, but the test is
  in `internal/provider/acpagent/lifecycle_test.go`, which S1's file list did not name. The
  comment had been edited before this was noticed. The edit touches the comment only, no code.
* **Decision:** the owner chose "Add the file to S1 (Recommended)". The file is added to S1's
  list, and the comment edit stays.

#### What was done

1. **The pin.** `go get github.com/maccavelli/acp-go-sdk2@v1.20.0`: "upgraded … v1.15.0 => v1.20.0".
   `go mod tidy` then printed nothing.
   * `go.mod` changed one line.
   * `go.sum` changed the SDK's two lines:
     `h1:RSZL1Kvpm1kAJdSPmoTC012074XZjCRwh7P2V/Lt16U=` and, for its `go.mod`,
     `h1:5WHwC+Nd7Wz7V0c0Xtz5MyYUpUKet1X9nHzLowXYhj4=`. These are the sums the MADR's probe
     recorded from the module proxy.
2. **The check.** `submitPrompt`'s `ctx.Err()` check and its comment are gone. A three-line
   comment says why there is no check.
   * The test's comment says it now pins the SDK's behaviour (deviation 1).
3. **0184's PLAN.**
   * The 2026-10-18 status note's bullet on #7 gets a dated note: #7 closed on 2026-10-18
     (`gh issue view 7`), and the check is gone.
   * The B3 amendment is marked superseded and never run.
4. **0184's MADR.** It has no status note, so the note on #7 went where the MADR keeps that
   check: E5's B9 bullet.

#### Verification, 2026-10-22, go1.27.3 on darwin/arm64

```text
go build ./...: exit 0
--- PASS: TestPromptSendsNothingWithAnEndedContext (0.50s)
--- PASS: TestGoModRequiresTaggedACPDirectly (0.04s)
vet-darwin: exit 0; vet-linux: exit 0; vet-windows: exit 0
make test: exit 0; 43 ok, 0 FAIL lines
make race: exit 0; 43 ok, 0 FAIL lines
make lint: exit 0 (golint ./cmd/... ./internal/..., no output)
make staticcheck: exit 0 (staticcheck v0.8.2, GOOS linux, darwin, windows)
make pre-add-check FILES="…/session.go …/lifecycle_test.go": 2 file(s) clean (gofmt, golint, govulncheck)
check_records.py --check: exit 0
```

* **Fail-first.** The test's failing half is the MADR's scratch probe of 2026-10-22: on `v1.15.0`
  with the check removed, "a prompt with an ended context put "session/cancel" on the wire".
  Nothing new was added that would need its own plant.
* **markdownlint.** The repository's config excludes MADR and PLAN files from its globs. Run on the
  two 0184 files under that config anyway, HEAD has 445 MD004 findings (asterisk lists against the
  pinned dash) and 1 MD010. S1 adds one MD004: the new bullet in 0184's MADR, in that file's own
  asterisk style. This plan lints clean.

#### Not done in S1

* **`make live-grok`.** It is D2's gate, and runs once, at S6 (the owner's Q2 answer). D2 is not
  done until then.
* **`make ci-windows`** on the Windows test host: S6.
* **The quota-free live grok tests** that 0184's B3 listed (`TestLiveGrokElicitationAtSessionStart`,
  `TestLiveGrokPlanAtSessionStart`). They are live runs against grok, so they wait for S6 with
  `make live-grok`.

S1 was committed by the owner as `a9741ef8`.

### S2 — 2026-10-22

#### What was done

1. **`Capture.Tap() io.Writer`.** For a nil `Capture` it returns an untyped nil. Otherwise it
   returns a `tapWriter`, whose `Write` always reports `len(p), nil`.
2. **One line splitter for both directions.** The splitting moved out of `teeReader` into a small
   `lines` type, which `teeReader` and `tapWriter` both use. It also changed
   `strings.IndexByte(string(buf), '\n')` to `bytes.IndexByte(buf, '\n')`, which finds the same
   index without converting the buffer to a string on each call.
   * This keeps one implementation of the framing until S3 removes `teeReader`.
   * `TestTeeReaderEmitsOnNewline` is unchanged and passes, and the new
     `TestTapRecordsWhatTeeReaderRecords` compares the two.
3. **Three tests**, in `wirecap_test.go`, with a `testCapture` helper:
   * `TestTapOfNilCaptureIsNilWriter`;
   * `TestTapRecordsWhatTeeReaderRecords`: the same four chunks through both, including a line
     split across writes and a last line with no newline;
   * `TestTapDoesNotRetainTheCallersSlice`.

No file outside S2's list changed.

#### Fail-first, on scratch copies of `internal/wirecap` (`s2/plants.py`)

```text
== control: no plant: PASS (as expected)
== typed nil from a nil Capture: FAIL (as expected)
   wirecap_test.go:210: nil Capture.Tap() = (*wirecap.tapWriter)(nil), want a nil io.Writer
== no buffering across writes: FAIL (as expected)
   wirecap_test.go:264: Tap frames = "hel\nlo\nne\nxt\n{\"a\":1}\ntail without newline\n", want "hello\nnext\n{\"a\":1}\n"
== the caller's slice retained: FAIL (as expected)
   wirecap_test.go:288: frames = "xyz\n", want "abc\n"
```

#### Verification, 2026-10-22

```text
go test -count=1 -v ./internal/wirecap/: ok (every test PASS, the three new ones included)
make pre-add-check FILES="internal/wirecap/wirecap.go internal/wirecap/wirecap_test.go": 2 file(s) clean (gofmt, golint, govulncheck)
make test: exit 0; 43 ok, 0 FAIL lines
make race: exit 0; 43 ok, 0 FAIL lines
```

S2 was committed by the owner as `4234e614`.

### S3 — 2026-10-22

#### Deviation 1 — 2026-10-22: two test files added to S3's files

* **Found, before any S3 edit:**
  * `internal/provider/acpagent/extensions_test.go:31` builds a session with `procExited: true`
    to skip the kill in `Close`. Step 4 removes the field, so the file would not compile. The
    same line in `session_test.go:136` and `prewarm_test.go:45` is in S3's list; this one was
    missed.
  * Step 7's check that the agent has a process group of its own needs `syscall.Getpgid`, which
    does not exist on Windows. The Windows build of the package's tests would not compile.
* **Decision:** the owner chose "Add both files (Recommended)".
  * `extensions_test.go` loses the `procExited` line. Its session has no process, so nothing is
    killed either way.
  * A new `agentproc_unix_test.go` (`//go:build unix`) holds the process-group assertion.
    `agentproc_test.go` keeps the parts that run everywhere.

#### Deviation 2 — 2026-10-22: the agent-death plant drops both watchers

* **Found, on a scratch copy (`s3/plants.py`):** step 7 planned to drop the exit watcher and see
  `TestSpawnedAgentDeathIsOneDisconnect` fail. It passed:
  `--- PASS: TestSpawnedAgentDeathIsOneDisconnect (0.23s)`.
  * `watchConnClose` sees the same death, since the agent's output ends with it, and sends the
    one disconnect.
  * That is the design both before and after S3: two watchers, one latch (`signalDisconnected`).
* **Decision:** the owner chose "Plant both watchers (Recommended)". With both dropped the test
  fails: "no disconnected status after the agent died". S4 tests each watcher's message on its
  own.

#### What was done

1. **`spawnAgent`** builds the command as before: `launch.Resolve`, `launch.Command`, `cmd.Dir`
   and the stderr `slogWriter`. Then:
   * it starts the agent with `agentproc.Command{Cmd, Grace: agentGrace, ConnectionOptions:
     clientConnOptions(log), OutputTap: s.wire.Tap()}.Start(s)`;
   * it sets `s.proc`, and `s.conn = proc.Conn()`.

   `agentGrace` is a 2 s constant, and its comment says what it bounds under `Kill`.
2. **Gone from `spawnAgent`:** the pipes, `cmd.Start`, `SuperviseStarted` and its release, and
   every `cmd.Wait`.
   * Spawn's four failure paths call `killTree`, now `proc.Kill()`, which kills and reaps.
   * The exit watcher waits on `proc.Wait()`. Its message is unchanged, for S4.
3. **`session`.**
   * `cmd`, `release` and `procExited` are replaced by `proc`, and `killProcessTree` is deleted.
   * `markClosedAndKill` and `Close` call a new `killProc`, which starts `proc.Kill()` on a
     goroutine. That keeps the old contract: signal, and let the watcher reap.
   * `Close`'s comment on recycled PIDs now says why there is nothing to guard: `agentproc` sends
     nothing to an agent it has reaped.
4. **`claimWarm`** treats a spare as dead when `s.closed` is set or `s.proc.Done()` is closed.
5. **`wirecap`.**
   * `TeeReader` and `teeReader` are deleted. `Tap`'s comment records that it replaced them.
   * `TestTeeReaderEmitsOnNewline` is deleted, and the `TeeReader` half of
     `TestNilCaptureMethodsNoop` with it.
   * S2's comparison test became `TestTapFramesLinesAcrossWrites`, with the same chunks and the
     same expected frames.
6. **Tests.** The test binary is its own agent: `TestMain` serves a minimal ACP agent when
   `MCREMOTE_TEST_ACP_AGENT=serve`, and exits at once for `exit`. The serving agent writes its PID to
   the file `MCREMOTE_TEST_ACP_AGENT_PIDFILE` names.
   * `TestSpawnAgentStartsThroughAgentproc`: initialize; the capture's first frame is the raw
     `initialize` answer; after `Close`, the agent is gone before `agentGrace` has passed.
   * `TestSpawnedAgentDeathIsOneDisconnect`: the agent is killed from outside by its PID. One
     error and one `disconnected` follow.
   * `TestSpawnedAgentHasItsOwnProcessGroup` (Unix): `getpgid(pid) == pid`.
   * `TestClaimWarmRejectsDeadSpare` now uses a reaped `exit` agent.
   * A new `TestClaimWarmRejectsClosedSpareWithoutProcess` covers a closed spare with no process.
7. `extensions_test.go` and `session_test.go` lose `procExited` (deviation 1).

#### Two assertions the plants caught as vacuous, fixed before the phase closed

* **Close ending the agent passed with `killProc` removed.**
  * The helper exited by itself at its input's end, which `Close` brings about.
  * The helper now sleeps a minute after its input ends, as a wedged agent would.
* **It still passed: `--- PASS … (2.05s)`.** `agentproc`'s own tie stops an agent whose connection
  ended, after the grace, with SIGTERM (G1).
  * The test now requires the agent gone before `agentGrace`.
  * Without `Kill` it fails: "the agent ended 2.005729667s after Close, not before agentproc's
    grace of 2s: Close did not kill it".

#### Fail-first, on scratch copies of the module (`s3/plants.py`), final run

```text
== control: no plant: PASS (as expected)
== preset Pgid: agentproc must refuse the start: FAIL (as expected)
   agentproc_test.go:141: start …/acpagent.test: agentproc: Command.Cmd.SysProcAttr.Pgid is set; the agent needs a process group of its own
== Close does not kill: FAIL (as expected)
   agentproc_test.go:155: the agent ended 2.005729667s after Close, not before agentproc's grace of 2s: Close did not kill it
== no tap: FAIL (as expected)
   agentproc_test.go:166: the capture's first frame is not the initialize answer: ""
== exit watcher only dropped (the PLAN's planned plant): PASS (recorded)
== both watchers dropped: FAIL (as expected)
   agentproc_test.go:202: no disconnected status after the agent died
== claimWarm ignores an exited process: FAIL (as expected)
   prewarm_test.go:52: dead spare must not be claimed
```

The process-group test has no Magic-side plant. On this path the group is `agentproc`'s to make,
and the preset-`Pgid` plant shows Magic's start goes through that check.

#### Verification, 2026-10-22, go1.27.3 on darwin/arm64

```text
vet-darwin: exit 0; vet-linux: exit 0; vet-windows: exit 0
make test: exit 0; 43 ok, 0 FAIL lines
make race: exit 0; 43 ok, 0 FAIL lines
make lint: exit 0
make staticcheck: exit 0
make pre-add-check FILES="(the nine S3 Go files)": 9 file(s) clean (gofmt, golint, govulncheck)
git grep -n -E "SuperviseStarted|KillProcessGroup|StdinPipe|StdoutPipe" -- internal/provider/acpagent:
  terminal.go:39, :111, :113 only
pgrep -fl acpagent.test after the runs: no process left
```

* **A whitespace-only fix landed after the gate run.** gofmt's alignment of `session`'s fields
  came after it, so `go test -race` was re-run on `acpagent` and `wirecap` with the final bytes:
  both ok.

#### Not done in S3

* **Windows and Linux.** The job, its suspended start and breakaway, and `Pdeathsig` were not run
  on their hosts. That is S6.
* **The exit watcher's message is unchanged.** Its text is S4's.

S3 was committed by the owner as `c4a39e85`.

### S4 — 2026-10-22

#### Deviation 1 — 2026-10-22: `watchConnClose` defers to the exit watcher when the agent has exited

* **Found, before any S4 edit, on a scratch copy (`s4/which_watcher.py`).** The helper agent was
  killed from outside 30 times. The error that reached the session's events was:

  ```text
    28  testagent agent connection lost
     2  testagent process exited: signal: killed
  ```

  * `agentproc`'s `Wait` returns only once the connection's goroutines have (`agentproc.go:298-313`).
    So the connection's end is nearly always reported first.
  * S4 as planned would have put `conn.Err()` on that message, and the exit status D11 asks for
    would almost never reach the phone.
* **Decision:** the owner chose "Prefer the exit watcher (Recommended)".
  * `watchConnClose`, after the connection ends, waits up to 500 ms for the process to exit.
  * If the process exits, it stays silent and the exit watcher reports `Wait`'s error.
  * If not, it reports `conn.Err()`.
* **Cost:** a connection that ends while its agent lives is reported up to 500 ms later, and
  `session.go` gains one branch with its test.
* **The MADR** gets a note on D11. Recording a deviation's change to a decision is what adds the
  MADR to S4's files.

#### What was done

1. **`watchConnClose`** (`session.go`).
   * Its parameter's interface gains `Err() error`.
   * After the connection ends it waits up to `exitPreference` (500 ms) for `s.proc.Done()`, and
     returns silently if the process exits.
   * Otherwise its message is `"<id> agent connection lost: <conn.Err()>"`.
   * Its comment no longer says an overflow tears the connection down: under `OverflowDropNewest`
     one cannot (MADR F6). It also gives the reason for the exit preference.
2. **The exit watcher** (`acpagent.go`) already carried `Wait`'s error after S3. Its comment now
   says so. The comment at `go s.watchConnClose(conn)` lost the overflow claim too.
3. **Tests.**
   * `fakeConn` gains `err` and `Err()`. `TestWatchConnCloseEmitsDisconnected` now requires the
     error `… agent connection lost: acp: peer disconnected`, and its comment no longer blames an
     overflow.
   * `TestSpawnedAgentDeathIsOneDisconnect` now requires the message `testagent process exited: …`,
     without `agent killed`.
   * New: `TestConnectionEndWithAgentAliveReportsTheConnection`. The connection is closed while
     the helper lives, and the message is `testagent agent connection lost: acp: connection
     closed`.
   * New: `TestClosedSessionReportsNoDisconnect`. After Magic's own `Close`, no error event in
     `exitPreference` plus 500 ms.
   * `TestSpawnedAgentDeathIsOneDisconnect` passed 30 of 30 with `-count=30`.

#### Fail-first, on scratch copies of the module (`s4/plants.py`), final run

```text
== control: no plant: PASS (as expected)
== the cause left out of the connection's message: FAIL (as expected)
   session_test.go:578: disconnect error = "grok agent connection lost", want the connection's end cause
== no wait for the exit: FAIL (as expected)   (--- FAIL 10 of 10)
   agentproc_test.go:210: the agent's death was reported as "testagent agent connection lost: acp: peer disconnected: peer connection closed"; want its exit status from the exit watcher
== an unbounded wait for the exit: FAIL (as expected)
   agentproc_test.go:249: the connection's end was reported as "testagent process exited: agentproc: agent terminated: signal: terminated"; want its cause
== a closed session still reports, latch only (recorded: emit also drops on a closed session): PASS (recorded)
== a closed session still reports, latch and emit both bypassed: FAIL (as expected)
   agentproc_test.go:275: a closed session reported "testagent process exited: agentproc: agent killed: signal: killed"
```

* **The plan's plant for a closed session needed both guards removed.** The plan's "emit on close"
  first ran as the latch alone, and passed. `emit` also drops every event once a session is closed
  (MADR 0057), so silence on close has two guards. With both bypassed, the test fails. The
  failure also shows D11's wording for Magic's own kill: `agentproc: agent killed: signal: killed`.

#### Verification, 2026-10-22, go1.27.3 on darwin/arm64

```text
vet-darwin: exit 0; vet-linux: exit 0; vet-windows: exit 0
make test: exit 0; 43 ok, 0 FAIL lines
make race: exit 0; 43 ok, 0 FAIL lines
make lint: exit 0
make staticcheck: exit 0
make pre-add-check FILES="(the four S4 Go files)": 4 file(s) clean (gofmt, golint, govulncheck)
go test -count=30 -run '^TestSpawnedAgentDeathIsOneDisconnect$': ok
pgrep -fl acpagent.test after the runs: no process left
```

#### Not done in S4

* **On Windows** the exit status of a killed agent reads differently (`exit status 1`). The death
  test checks only for `testagent process exited:` and the absence of `agent killed`, so it holds
  there.
  It is run at S6 with `make ci-windows`.
