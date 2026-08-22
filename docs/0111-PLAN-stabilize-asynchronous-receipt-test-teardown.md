<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Implement stabilization of asynchronous receipt test teardown

Associated MADR: [0111-MADR-stabilize-asynchronous-receipt-test-teardown.md](0111-MADR-stabilize-asynchronous-receipt-test-teardown.md)

## Goal

Make `TestRespondPermissionReturnsBeforeReceiptRoundTrip` deterministic under
`go test -race` while retaining its assertion that `RespondPermission` does
not block on receipt signing.

## Scope

### Files to change

* `internal/session/manager_receipt_test.go`

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

## Rollout and Rollback

The change affects test execution only. It needs no runtime migration,
configuration change, feature flag, or release-note entry.

After the implementation is approved, committed, and reaches `master`, a
normal CI run verifies the repair in the hosted environment. Releasing or
recreating `v0.14.5` remains outside this plan and requires explicit owner
authorization.

If the ordered completion reveals a real receipt persistence failure rather
than completing successfully, stop implementation. Record the observed error
and amend this MADR and plan before changing production receipt code.
