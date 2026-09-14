<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Implement stabilization of asynchronous receipt test teardown

Associated MADR: [0111-MADR-stabilize-asynchronous-receipt-test-teardown.md](0111-MADR-stabilize-asynchronous-receipt-test-teardown.md)

## Goal

Make `TestRespondPermissionReturnsBeforeReceiptRoundTrip` deterministic under
`go test -race` while retaining its assertion that `RespondPermission` does
not block on receipt signing. Following the first hosted verification run,
also replace the independent receipt-store wall-clock gate with deterministic
coverage of the invariants that gate was intended to protect.

## Scope

### Files to change

* `internal/session/manager_receipt_test.go`
* `internal/receipt/store_test.go` (P2 amendment)

### Files not to change

* `internal/session/manager.go`
* `internal/receipt/store.go`
* `.github/workflows/ci.yml`
* Production configuration, release, or provider code

### Non-goals

* Introducing production tracking or shutdown waiting for receipt goroutines.
* Changing receipt persistence behavior, timeouts, or error handling.
* Weakening the test's 200-millisecond prompt-return threshold.
* Retrying or publishing the failed `v0.14.5` release; those actions require
  separate owner direction.
* Changing receipt durability (`f.Sync`), storage format, append behavior, or
  production performance based on one uncontrolled CI timing ratio.

## Evidence Update After P1 Rollout

P1 was implemented in commit `e4497c7`. Hosted CI run 32549470400 confirmed
that `internal/session` now passes under `go test -race ./...`; the original
cleanup failure did not recur.

That run failed independently in `internal/receipt.TestStoreScalesLinearly`:
the first 200 operations averaged 1.345043 ms each and the next 800 averaged
7.776681 ms each, crossing the hard 5x wall-clock ratio. MADR 0111 F1-F12
records the full code and test evidence. In summary:

* the timed helper includes ECDSA signing and one durable `f.Sync` per entry;
* the store's hot `LastHash` path is a map lookup and `Append` uses `O_APPEND`;
* full-chain verification runs after both timing samples;
* 10 sequential focused, 12 concurrently loaded focused, and 3 full-suite
  local race executions all passed;
* the test and PLAN 0077 both describe the timing as "not a hard perf gate",
  despite the test enforcing it with `t.Fatalf`.

## Implementation Steps

### Phase P1 — Order asynchronous test completion before cleanup

1. In `TestRespondPermissionReturnsBeforeReceiptRoundTrip`, retain the blocked
   fake transport and the existing measurement around `RespondPermission`.
2. After the existing assertion that the response returned in at most 200
   milliseconds, close `release` explicitly. Do not defer this close: the
   test must control when the signing path is permitted to continue.
3. Use the existing `waitForReceipt` helper to wait up to two seconds until
   `fx.rcptStore.LastHash(fx.deviceID)` returns `err == nil` and `ok == true`.
   This observes durable receipt persistence, which happens after the
   background goroutine has completed its file writes.
4. Return only after the wait succeeds. The fixture's `t.Cleanup` and
   `t.TempDir` cleanup will therefore run after the receipt writer is done.
5. Keep the test's transport and fixture local; do not add synchronization to
   `session.Manager` or the production receipt store.

#### P1 result

Implemented and committed as `e4497c7`. The focused race test passed 50
consecutive local runs, the `internal/session` race suite passed locally, the
full race suite passed locally, and the affected package passed in hosted CI.

### Phase P2 — Replace the nondeterministic receipt-store timing gate

This phase was proposed by the post-P1 amendment and approved by the owner on
2026-08-21.

1. In `internal/receipt/store_test.go`, replace
   `TestStoreScalesLinearly`'s two-window elapsed-time ratio with a large-chain
   functional test. Retain 1,000 sequential synthetic appends and the final
   `Store.Verify` assertion so a substantial chain must remain intact.
2. Remove `firstElapsed`, `secondElapsed`, per-entry division, and the 5x
   `t.Fatalf`. Routine unit tests must not infer an asymptotic regression from
   filesystem and scheduler wall time.
3. Add a focused deterministic test for the hot-cache invariant:
   * append an entry and obtain its expected last hash;
   * remove the underlying JSONL file;
   * call `LastHash` on the same `Store` and require the cached hash and
     `ok=true`;
   * construct a fresh `Store` over the same directory and require no hash,
     proving the first result came from memory rather than a hidden disk read.
4. Retain `TestStoreLastHashSurvivesRestart`, which independently proves the
   complementary cold-cache path can recover the final line from disk.
5. Do not modify `Store`, `Append`, `LastHash`, `readLastLine`, receipt
   durability, the workflow, or timing thresholds elsewhere.

## Verification

Run these commands after P1, in order:

```bash
gofmt -w internal/session/manager_receipt_test.go
go test -race ./internal/session -run '^TestRespondPermissionReturnsBeforeReceiptRoundTrip$' -count=50
go test -race ./internal/session
go test -race ./...
make pre-add-check FILES="internal/session/manager_receipt_test.go"
```

Acceptance criteria:

* The repeated focused race run completes without `TempDir RemoveAll cleanup`
  failures or receipt-store write-after-cleanup warnings.
* The focused and package race tests pass.
* The full repository race suite passes, matching CI.
* `gofmt` and the repository pre-add checks report no issue for the changed Go
  file.

### P2 verification commands

Run these commands after the approved P2 implementation, in order:

```bash
gofmt -w internal/receipt/store_test.go
go test -race ./internal/receipt -run '^TestStore(LastHashUsesHotCache|LargeChainMaintainsIntegrity)$' -count=20
go test -race ./internal/receipt
go test -race -count=3 ./...
make pre-add-check FILES="internal/receipt/store_test.go"
```

P2 acceptance criteria:

* The large-chain test appends and verifies 1,000 entries without a wall-clock
  pass/fail condition.
* The hot-cache test proves a populated `Store` returns its cached hash even
  after its backing chain is removed, while a fresh store does not.
* Existing restart, permissions, tamper-detection, signing, and receipt tests
  remain green.
* Three complete repository race-suite passes succeed locally.
* Pre-add checks pass for `internal/receipt/store_test.go`.

#### P2 result

Implemented on 2026-08-21. Both deterministic tests passed 20 consecutive
focused race-mode runs, the complete `internal/receipt` race suite passed,
and `go test -race -count=3 ./...` passed. The repository pre-add check passed
for `internal/receipt/store_test.go`; `govulncheck` could not reach its database
and was skipped under the repository's documented offline policy. Hosted CI
verification remains pending until this phase is pushed.

## Rollout and Rollback

The change affects test execution only. It needs no runtime migration,
configuration change, feature flag, or release-note entry.

P1 is already committed and on `master`. P2 is approved and implemented
locally; after it is committed and reaches `master`, a normal CI run verifies
both affected packages in the hosted environment. Releasing or recreating
`v0.14.5` remains outside this plan and requires explicit owner authorization.

If P2's deterministic cache or chain checks reveal a real receipt persistence
failure, stop implementation. Record the observed error and amend this MADR
and plan before changing production receipt code. Rollback of P2 is a revert
of its test-only commit; it requires no data or configuration rollback.
