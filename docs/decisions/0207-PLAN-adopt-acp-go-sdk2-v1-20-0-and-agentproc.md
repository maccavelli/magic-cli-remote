---
status: proposed
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
     * Plant: drop the watcher. The test must fail.
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
