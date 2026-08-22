---
status: accepted
date: 2026-08-21
decision-makers: Project Owner
consulted: GitHub Actions failure log; local race-test reproduction
informed: Implementers and maintainers of internal/session receipt tests
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Stabilize asynchronous receipt test teardown

## Context and Problem Statement

The `v0.14.5` tag CI run failed in the Go job's `go test -race ./...` step,
before release binaries or assets were built. The failed run was for commit
`e659af2fffbdaf575ac1ae115207328f60dfefe9`; the `master` CI run for that
same commit succeeded minutes earlier. The differing outcomes establish that
the failure is nondeterministic and is not caused by tag-only build or release
configuration.

At the failing commit, the test was
`TestRespondPermissionReturnsBeforeReceiptRoundTrip` in
`internal/session/manager_receipt_test.go`. Its purpose is correct: prove that
`Manager.RespondPermission` returns without waiting for the asynchronous
receipt round trip. It blocked the fake receipt transport on `release`,
scheduled `defer close(release)`, and returned after the prompt-return-time
assertion.

The fixture creates its receipt store in `t.TempDir()`
(`internal/session/manager_receipt_test.go:130-167`). After the deferred close
unblocks the receipt goroutine, that goroutine can still archive the device
key and append the signed receipt while Go test cleanup removes the temporary
directory. The failed CI log reports exactly this concurrent teardown:

```text
--- FAIL: TestRespondPermissionReturnsBeforeReceiptRoundTrip (0.30s)
    testing.go:1464: TempDir RemoveAll cleanup: .../receipts: directory not empty
```

The failure is reproducible locally with:

```bash
go test -race ./internal/session \
  -run '^TestRespondPermissionReturnsBeforeReceiptRoundTrip$' -count=50
```

That run produced the same `TempDir RemoveAll cleanup` failure and receipt
store warnings showing attempted writes after the temporary directory had been
removed. The evidence shows a test teardown race, not a race detector report
against production memory access and not a receipt-store correctness failure.

## Decision Drivers

* Preserve the test's intended assertion that responding to a permission does
  not wait for a receipt signing round trip.
* Ensure no background receipt writer can access fixture-owned files after the
  fixture begins cleanup.
* Keep the remediation narrowly scoped to the test; production lifecycle
  semantics must not be changed solely to accommodate a test fixture.
* Make the expected asynchronous completion observable with bounded failure
  behavior rather than an arbitrary post-release sleep.
* Verify the repaired test under the same `-race` mode CI uses.

## Considered Options

* Option 1: Leave the test unchanged and retry failed tag CI runs
* Option 2: Add manager-wide receipt-goroutine tracking and shutdown waiting
* Option 3: Release the test transport and wait for durable receipt completion
  before fixture cleanup (chosen)
* Option 4: Add a fixed delay after releasing the test transport

## Decision Outcome

Chosen option: "Option 3: release the test transport and wait for durable
receipt completion before fixture cleanup", because it proves both required
properties in their proper order: `RespondPermission` returns while signing is
blocked, then the test allows and observes completion of the deliberately
asynchronous operation before its temporary filesystem is removed.

### Consequences

* Good, because the test retains the non-blocking production contract it was
  written to protect.
* Good, because completion is based on the receipt store's observable state,
  which occurs after the background operation's file writes, rather than on a
  scheduler-dependent delay.
* Good, because the production `Manager` and receipt lifecycle APIs do not
  acquire test-only synchronization responsibilities.
* Bad, because this one test takes a bounded poll interval to observe its
  asynchronous effect.
* Neutral, because the test already has `waitForReceipt`, whose two-second
  bound and ten-millisecond poll interval are used by the other receipt tests
  in the same file (`:211-224`).

### Confirmation

The implementation is confirmed when all of the following are true:

* The test explicitly releases the fake transport only after asserting the
  prompt response returned promptly.
* It waits for a receipt entry to be durably visible before returning, so the
  fixture's `t.TempDir` cleanup cannot overlap the writer.
* The focused test passes repeatedly with `-race` and no receipt-store
  write-after-cleanup warnings.
* `go test -race ./...` passes, matching the CI step at
  `.github/workflows/ci.yml:107-108`.

## Pros and Cons of the Options

### Option 1: Leave the test unchanged and retry failed tag CI runs

* Good, because it changes no code immediately.
* Bad, because it leaves nondeterministic release failures and does not repair
  the test's invalid teardown.

### Option 2: Add manager-wide receipt-goroutine tracking and shutdown waiting

* Good, because a production shutdown barrier can be valuable if it is an
  independently required lifecycle guarantee.
* Bad, because no evidence requires such a production contract for this CI
  failure, and it expands runtime shutdown behavior beyond the defect.

### Option 3: Release the test transport and wait for durable receipt completion

* Good, because it directly orders the test's release, write completion, and
  temporary-directory cleanup.
* Good, because it validates a real observable effect of the receipt round
  trip rather than an implementation-specific goroutine signal.
* Bad, because the test must include an additional bounded wait.

### Option 4: Add a fixed delay after releasing the test transport

* Good, because it is a small textual change.
* Bad, because timing delays cannot establish completion and would merely move
  the flake to slower CI runners.

## More Information

The associated implementation plan is
[0111-PLAN-stabilize-asynchronous-receipt-test-teardown.md](0111-PLAN-stabilize-asynchronous-receipt-test-teardown.md).

The release workflow was not selected for modification: the same Go test step
is already correctly configured to run `go test -race ./...`, and the failure
occurred before tag-only build, artifact, and publishing steps.
