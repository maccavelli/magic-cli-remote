---
status: complete
date: 2026-10-30
associated-madr: "0218-MADR-report-not-live-when-a-session-closes-during-a-call.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement reporting a session as not live when it closes during a call

Associated MADR: [0218-MADR-report-not-live-when-a-session-closes-during-a-call.md](0218-MADR-report-not-live-when-a-session-closes-during-a-call.md)

## Goal

A Manager operation whose session is closed, replaced, or marked dead while its provider call is
in flight returns `ErrNotLive`, wrapping the provider's error. This covers all 19 callers of
`liveSession` (MADR F1, D1, D2). `TestPromptFailsAfterDisconnectAutoClose` then passes on Windows
because the race is gone.

## Scope

**Files:**

* `internal/session/manager.go`: `providerErr`, the 19 callers, and D3's comment at
  `closeMatching`;
* `internal/session/manager_providererr_test.go` (new, `package session`): D4's helper test;
* `internal/session/manager_closerace_test.go` (new, `package session_test`): D4's `Prompt` and
  `Cancel` tests;
* this pair.

**Not in this commit:**

* `docs/README.md`. The index is regenerated with
  `0217-PLAN-update-every-pin-to-its-current-release.md`'s P1, whose rows it already carries
  uncommitted. The records check does not require a row, and passed with unindexed records
  before;
* 0217's P1 files in the working tree (`ci.yml`, `apps/mobile/README.md`, the 0169 PLAN, the
  0217 pair). They stay unstaged.

**Out:** MADR D5. That is the process-death window, a typed `provider.ErrClosed`, and the
operations that do not use `liveSession`.

## Constraints

* **C1 — Seen failing first, on a scratch copy.** The new tests are shown failing against the old
  behaviour in a copy of the tree, never in the tree (the owner's standing rule).
* **C2 — Stage only.** The owner commits with `git commit --no-edit`. `make pre-add-check
  FILES="…"` passes on each staged `.go` file first.
* **C3 — Host runs** are covered by the owner's word for 0217's C2, which this pair gates. Only
  scratch trees are used on the hosts, and they are removed afterwards.
* **C4 — No identifier in a record.** `leftovers/guard_preview.py` runs on both records before
  staging.
* **C5 — A deviation is a stop** (AskUserQuestion, then a dated entry here).

## Implementation Steps

### S1 — The tests, seen failing first (D4)

1. Build a scratch copy at `leftovers/copies/0218-red`: `git archive HEAD`, with nothing laid over
   it.
2. Into the copy only, add:
   * the two new test files, as they will be committed;
   * a stand-in `providerErr` that returns `err` unchanged. That is today's behaviour, so the
     helper test compiles and fails on its assertions, not on a missing symbol.
3. Run `go test -count=1 -run 'ProviderErr|CloseRace' ./internal/session/` in the copy. Expected:
   * the helper test fails in each `ErrNotLive` case (removed, replaced, dead), and passes the
     live and nil cases;
   * `Prompt` and `Cancel` each fail with `got session closed`, every time. Run them with
     `-count=20` to show that.

   The output is recorded.
4. **On the Windows test host** (C3), in a scratch tree of HEAD alone: run
   `go test -run '^TestPromptFailsAfterDisconnectAutoClose$' -count=300 ./internal/session/`, and
   record the number of failures as found. A count of 0 is recorded as such; it shows only that
   the window was not hit that time. The deterministic tests in step 3 are the proof.

### S2 — The fix (D1 to D3)

1. **`providerErr`,** beside `liveSession` (`manager.go:2657`), as MADR D1 gives it:
   * nil in, nil out;
   * under `m.mu.RLock`: when the entry is missing, is `dead`, or `e.sess != sess`, return
     `fmt.Errorf("%w: %q: %w", ErrNotLive, id, err)`;
   * otherwise return `err` unchanged.
2. **The 19 callers** (MADR F1, at the lines given there). In each, the error of each call made on
   the session, or on an interface asserted from it, is returned as `m.providerErr(id, sess, err)`.
   * `Prompt` keeps `m.clearPromptStart(id)` before its return.
   * `Cancel`'s `return sess.Cancel(ctx)` becomes an assignment, then the helper.
   * Returns of the manager's own errors (unsupported capability, validation) are left as they are.
   * `workspaceSession` and `shareSession` return a session, not a call result, so their callers'
     calls on what they return are routed instead. The edit reads each caller to find those
     calls.
3. **D3's comment** at `closeMatching`'s delete (`:3494`): `providerErr` relies on the entry being
   removed before `Close` runs.
4. Every edit is read back, and `git diff --stat internal/session/manager.go` is recorded.

### S3 — The tests in the tree (D4)

1. **`manager_providererr_test.go`** (`package session`) builds a `Manager` with one entry, and
   cases for: the entry removed, the entry holding another session, the entry `dead`, the entry
   live, and a nil error.
   * Each `ErrNotLive` case also checks `errors.Is(got, providerSentinel)`.
   * The live case checks that the same error value comes back.
2. **`manager_closerace_test.go`** (`package session_test`) adds a `gateProvider`, whose session's
   `Prompt` and `Cancel` signal entry on a channel, then wait for a release channel. After that
   they return `errors.New("session closed")` if `Close` has run, and nil otherwise. For each of
   `Prompt` and `Cancel`:
   1. create a session, and start the call in a goroutine;
   2. wait for the entry signal;
   3. emit `StatusDisconnected`;
   4. wait until the fake's close count is 1 (F2: the entry is deleted by then);
   5. release the call;
   6. assert `errors.Is(err, session.ErrNotLive)`, and that the error text still contains
      "session closed".

   Every wait is bounded (2 s), and fails with what it was waiting for. No sleep decides an
   outcome.
3. `TestPromptFailsAfterDisconnectAutoClose` is unchanged.

### S4 — Gates

1. **This Mac:**
   * `make pre-add-check FILES="internal/session/manager.go internal/session/manager_providererr_test.go internal/session/manager_closerace_test.go"`;
   * `go vet ./...` for darwin/arm64, windows/amd64 and linux/amd64 with cgo off;
   * `make staticcheck`;
   * `go test -race -count=1 ./internal/session/...`, then `go test -race -count=20 -run 'ProviderErr|CloseRace|TestPromptFailsAfterDisconnectAutoClose' ./internal/session/`;
   * `make race`;
   * `make check-records`.
2. **The test hosts** (`leftovers/pins_hosts.py`, the S2 and S3 files laid over HEAD):
   * Linux: `go test -race ./...`;
   * Windows: `make ci-windows`, which must pass A6 and every check after it.
3. **On the Windows test host,** with the fix: step S1.4's `-count=300` run must report 0
   failures.

### S5 — Records and staging

1. Write this plan's execution record: the S1 outputs, S1.4's count, the S2 diff stat, and the S4
   results.
2. Set this plan to `complete` and the MADR to `accepted`, once every acceptance criterion below
   is met.
3. Run the guard preview on both records. Stage exactly the files in Scope, and check with
   `git diff --cached --name-only`.
4. The owner commits. Then 0217's P1 resumes at its step 6: its Windows gate is run again on the
   new HEAD.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| A1 | On the scratch copy without the fix, the helper test fails in its three `ErrNotLive` cases, and `Prompt` and `Cancel` fail 20 of 20 runs | Confirmation |
| A2 | With the fix, the new tests and `TestPromptFailsAfterDisconnectAutoClose` pass 20 of 20 runs under `-race` on this Mac | D4 |
| A3 | All 19 callers route their provider errors through `providerErr`. Each is listed in the execution record | D2 |
| A4 | `make race` passes here. `go test -race ./...` passes on the Linux test host. `make ci-windows` passes on the Windows test host | Confirmation |
| A5 | S1.4's count is recorded before the fix, and is 0 after it | Confirmation |

## Rollout and Rollback

* One commit, landed before 0217's P1 commit.
* Rollback: revert it. The race returns, and nothing else changes: no wire, storage or protocol
  shape moves. `ErrNotLive` maps to the `session_not_live` code the phone already knows.

## Execution record, 2026-10-30

**Approval:** "accept the 0218 madr, approved to proceed with the plan". The host runs use the
owner's word given for 0217's C2.

### S1 — seen failing first

The scratch copy `leftovers/copies/0218-red` is `git archive` of `533a6055`, plus the two test
files and the stand-in `providerErr` (returns `err` unchanged). Running
`go test -count=1 -run 'ProviderErr|CloseRace' -v ./internal/session/` there:

```text
=== RUN   TestProviderErr/removed
    manager_providererr_test.go:53: got session closed, want ErrNotLive
=== RUN   TestProviderErr/replaced
    manager_providererr_test.go:53: got session closed, want ErrNotLive
=== RUN   TestProviderErr/dead
    manager_providererr_test.go:53: got session closed, want ErrNotLive
    --- PASS: TestProviderErr/live (0.00s)
    --- PASS: TestProviderErr/nil_error (0.00s)
    manager_closerace_test.go:148: prompt returned session closed, want ErrNotLive
    manager_closerace_test.go:148: cancel returned session closed, want ErrNotLive
FAIL    github.com/maccavelli/magic-cli-remote/internal/session    0.720s
```

With `-count=20 -run CloseRace`, the result was `prompt: FAIL x20` and `cancel: FAIL x20`.

**S1.4, the Windows test host, without the fix:**
`go test -run '^TestPromptFailsAfterDisconnectAutoClose$' -count=300` gave
`TestPromptFailsAfterDisconnectAutoClose: PASS x300`. The window was not hit with the test run
alone. The failure of 2026-10-30, and CI's of 2026-10-24, both came under the whole package's load
in `make ci-windows`. Recorded as found: the deterministic tests above are the proof, as the plan
says.

### S2 — the fix

* `providerErr` is beside `liveSession`, as MADR D1 gives it, with a doc comment naming the rule
  for callers.
* D3's comment is at `closeMatching`'s delete.
* `git diff --stat internal/session/manager.go` reports `81 +++++++++++++++++++++++++++++++--------------`, `57 insertions(+), 24 deletions(-)`.

**A3, every caller routed.** `leftovers/route_check.py` lists each function that takes a session
through `liveSession`, `workspaceSession` or `shareSession`, and flags a provider-call error that
escapes unrouted.

* On the tree it reports `23 funcs, 0 not ok`. Those 23 are the 17 direct callers in F1 (all but
  the two helpers), plus the helpers' six callers: `CurrentShare`, `Share`, `Unshare`,
  `ListWorkspace`, `ReadWorkspace` and `SearchWorkspace`.
* It was seen failing first on the pre-fix copy: `23 funcs, 23 not ok`, with each escape named, for
  example `return sess.Cancel(ctx)` and `return ws.SearchWorkspace(ctx, kind, query)`.
* Its first run on the tree flagged `return m.handleUnsandboxedBang(...)` in `Prompt`. That is the
  manager's own method, which routes inside itself (`:1032`), not a provider call. The check now
  skips calls on the receiver `m`.

**Observed, not changed (MADR D5):** `Rename` does not use `liveSession`. It re-checks the entry
after a successful provider call, but returns `rs.Rename`'s error unrouted. It is outside this
pair's scope, and is named here for a later record.

### S3 — the tests in the tree

The two files from S1 are copied unchanged into `internal/session/`.
`TestPromptFailsAfterDisconnectAutoClose` is unchanged. With the fix:
`go test -race -count=20 -run 'ProviderErr|CloseRace|TestPromptFailsAfterDisconnectAutoClose' -v ./internal/session/`
passes every case 20 of 20 times. That covers the five helper cases, `prompt` and `cancel`, and the
old test.

### S4 — gates

| Gate | Result |
| :--- | :--- |
| `make pre-add-check FILES="…"` (the three Go files) | "go-precheck: 3 file(s) clean (gofmt, golint, govulncheck)." |
| `go vet ./...` (`leftovers/vet_goos.py`) | darwin/arm64, windows/amd64 with cgo off, and linux/amd64 with cgo off: each exit 0 |
| `make staticcheck` | exit 0, staticcheck v0.8.2 on linux, darwin and windows. Each reports "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name", the standing allowlist |
| `make race` | exit 0: 45 `ok` packages, and no `FAIL`, `DATA RACE` or `panic:` line |
| The Linux test host, `go test -race -count=1 ./...` (HEAD plus the three files) | `go1.27.3 linux/amd64`, exit 0, 45 `ok` lines |
| The Windows test host, `make ci-windows` (the same tree) | `go1.27.3 windows/amd64`, exit 0. A3, A2, A4, A5 and A6 each `PASS`, then "ci-windows-local: ALL SELECTED CHECKS PASSED" |
| The Windows test host, S1.4's `-count=300`, with the fix (`leftovers/copies/0218-green`) | `TestPromptFailsAfterDisconnectAutoClose: PASS x300` |

Every host tree was removed afterwards, and its absence checked.

### Acceptance

| # | Met | Evidence |
| :--- | :--- | :--- |
| A1 | yes | S1: three helper cases failed; `prompt` and `cancel` failed 20 of 20 |
| A2 | yes | S3: every case passed 20 of 20 under `-race` |
| A3 | yes | S2: `route_check.py`, 23 of 23 routed, and seen failing on the pre-fix copy |
| A4 | yes | S4: `make race`, the Linux test host, and `make ci-windows` |
| A5 | yes, with a note | 0 failures in 300 before the fix as well as after. The window needs package-level load, which the deterministic tests do not depend on |

**Files for this commit:**

* `internal/session/manager.go`;
* `internal/session/manager_providererr_test.go`;
* `internal/session/manager_closerace_test.go`;
* this plan and its MADR.

`docs/README.md` waits for 0217's P1 commit, as the Scope says.
