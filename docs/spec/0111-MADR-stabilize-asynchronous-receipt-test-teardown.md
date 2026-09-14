---
status: accepted
date: 2026-08-21
decision-makers: Project Owner
consulted: GitHub Actions failure logs; local sequential, concurrent, and full-suite race stress; receipt store implementation and history
informed: Implementers and maintainers of internal/session and internal/receipt tests
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

### Post-implementation CI evidence and correction

Phase P1 was implemented in commit `e4497c7` and pushed to `master`. The
resulting CI run ([GitHub Actions run 32549470400](https://github.com/maccavelli/magic-cli-remote/actions/runs/32549470400))
failed, but it did **not** reproduce the session teardown fault this record
originally addressed. The evidence from that run is:

| ID | Finding | Evidence | Confidence |
| --- | --- | --- | --- |
| **F1** | P1 implemented the planned ordering, and its target passed in hosted CI. | The test now asserts prompt-return latency, closes `release`, and waits on `LastHash` before returning (`manager_receipt_test.go:489-500`). `internal/session` completed successfully in 6.124 seconds under `go test -race ./...`; there was no `TestRespondPermissionReturnsBeforeReceiptRoundTrip` failure and no `TempDir RemoveAll cleanup` error. | Confirmed by code and CI log. |
| **F2** | The new failure is in a different package and test. | `internal/receipt.TestStoreScalesLinearly` failed; every reported package through `internal/providerauth` had passed, `internal/session` subsequently passed, and the final command failed only because `internal/receipt` failed. | Confirmed by CI log. |
| **F3** | The receipt test failed a relative wall-clock threshold, not data integrity or the race detector. | The first 200 appends averaged 1.345043 ms each; the next 800 averaged 7.776681 ms each. The ratio was 5.781734x, exceeding the test's 5x threshold at `internal/receipt/store_test.go:345-348`. The log contains no `DATA RACE`, signature failure, chain break, panic, or store error. | Confirmed by CI log. |
| **F4** | The timed region measures substantially more than receipt-store lookup complexity. | `appendPermissionDecision` (`store_test.go:31-60`) calls `LastHash`, builds and JSON-marshals a statement, calls `SignES256Compact`, and calls `Append`. Signing invokes `ecdsa.Sign(rand.Reader, ...)` (`jws.go:42-47`). `Append` opens the file, writes one line, calls `f.Sync`, and closes it (`store.go:238-264`). | Confirmed by code. |
| **F5** | The hot receipt-store path does not scan the growing chain. | `lastHashLocked` returns from the `lastHash` map on a hit (`store.go:162-176`). `Append` calls that helper while holding the store mutex and updates the map after each successful append (`store.go:238-264`). The file is opened with `O_APPEND`; there is no read-modify-rewrite loop. | Confirmed by code. |
| **F6** | The only cold lookup is bounded independently of chain length. | On a cache miss, `readLastLine` reads backward in fixed 4 KiB chunks (`store.go:22-26`, `:179-230`). In this test the first successful append populates the cache, so later appends use F5's map hit. | Confirmed by code. |
| **F7** | Full-chain verification is not part of either timing sample. | The test records both elapsed intervals at `store_test.go:315-325`, then calls `Verify` at `:327`; verification cannot make the second measured interval slower. | Confirmed by code. |
| **F8** | Ten sequential local race-mode samples did not show size-correlated growth. | `go test -race -v ./internal/receipt -run '^TestStoreScalesLinearly$' -count=10` passed all ten. First-batch averages ranged from 4.774 to 5.228 ms; second-batch averages ranged from 4.690 to 5.189 ms. | Confirmed by local reproduction on 2026-08-21. |
| **F9** | Twelve samples under deliberate concurrent receipt-test load also passed. | Four simultaneous processes each ran the focused race test three times. First-batch averages ranged from 11.872 to 15.125 ms; second-batch averages ranged from 14.344 to 15.109 ms; all ratios stayed below 1.24x despite approximately tripled absolute latency. | Confirmed by local stress on 2026-08-21. |
| **F10** | The real full-suite execution shape passed repeatedly locally. | `go test -race -count=3 ./...` passed all packages and all three `TestStoreScalesLinearly` executions. `internal/receipt` completed the three runs in 32.745 seconds. | Confirmed by local full-suite stress on 2026-08-21. |
| **F11** | The test contradicts its own and its originating plan's stated role. | Its comment says the check is "not a hard perf gate" (`store_test.go:287-300`), and PLAN 0077 says the 10,000-entry timing was a smoke check, "not a hard perf gate" (`docs/0077-PLAN-signed-receipts-permission-handoffs.md:375-380`). The test nevertheless calls `t.Fatalf` on a wall-clock ratio. | Confirmed by code and repository history. |
| **F12** | The wall-clock comparison uses two non-overlapping time windows and unequal sample counts. | The first sample measures 200 operations before the second sample measures 800. A runner-level change in filesystem sync latency, CPU scheduling, entropy service, or co-scheduled package load between those windows is indistinguishable from size-dependent store growth. | Confirmed by test structure; the source of the CI runner's latency change is not directly observable from the log. |

F4-F7 prove that the specific quadratic regression named in the test comment —
re-reading an ever-growing receipt file for every append — is not present in
the implementation under test. F8-F10 show constant per-entry behavior across
25 additional race-mode executions, including concurrent pressure and the
same full-suite command CI uses. Those results do not prove all possible
performance properties on every filesystem; they do establish that the CI
failure is insufficient evidence of quadratic application code.

The earlier wording "the fix is confirmed when `go test -race ./...` passes"
also needs correction. A repository-wide run is an integration gate, but its
failure in an unrelated package does not disprove the P1 synchronization
change. F1 directly confirms P1 on the hosted runner. The new CI blocker is a
separate nondeterministic assertion discovered by that rollout.

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
* Keep hard CI assertions deterministic and attributable to application
  behavior; do not infer algorithmic complexity from two noisy wall-clock
  windows that include cryptography and durable filesystem syncs.
* Preserve deterministic coverage for large-chain integrity and the hot hash
  cache so removing the timing ratio does not remove the invariant it intended
  to protect.

## Considered Options

* Option 1: Leave the test unchanged and retry failed tag CI runs
* Option 2: Add manager-wide receipt-goroutine tracking and shutdown waiting
* Option 3: Release the test transport and wait for durable receipt completion
  before fixture cleanup (chosen)
* Option 4: Add a fixed delay after releasing the test transport
* Option 5: Retry CI or raise the receipt test's timing multiplier
* Option 6: Optimize the production receipt store based on the failed ratio
* Option 7: Replace the hard wall-clock ratio with deterministic large-chain
  integrity and hot-cache assertions (accepted amendment)

## Decision Outcome

Chosen option: "Option 3: release the test transport and wait for durable
receipt completion before fixture cleanup", because it proves both required
properties in their proper order: `RespondPermission` returns while signing is
blocked, then the test allows and observes completion of the deliberately
asynchronous operation before its temporary filesystem is removed.

### Amendment outcome after run 32549470400

Chosen option: "Option 7: replace the hard wall-clock ratio with deterministic
large-chain integrity and hot-cache assertions", because the code and stress
evidence show that the named quadratic read path is absent, while the current
test can fail solely when unrelated work changes the second timing window's
latency. Performance timing may remain available as a non-gating benchmark or
diagnostic, but it must not decide routine CI correctness.

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
* Good, because the amendment makes a CI failure identify a violated store
  invariant rather than an unexplained change in runner timing.
* Good, because a large synthetic chain still exercises append and full-chain
  verification, and a hot-cache test directly proves that `LastHash` does not
  consult the file after an append.
* Bad, because deterministic unit tests cannot enforce a universal filesystem
  latency or asymptotic-performance budget; that remains a benchmark and code
  review responsibility.

### Confirmation

The implementation is confirmed when all of the following are true:

* The test explicitly releases the fake transport only after asserting the
  prompt response returned promptly.
* It waits for a receipt entry to be durably visible before returning, so the
  fixture's `t.TempDir` cleanup cannot overlap the writer.
* The focused test passes repeatedly with `-race` and no receipt-store
  write-after-cleanup warnings.
* `internal/session` passes in hosted CI, directly confirming the affected
  package and teardown path. The repository-wide `go test -race ./...` remains
  a release gate, but an unrelated package failure is reported separately
  rather than treated as evidence that P1 failed.
* After the amendment, `internal/receipt` retains a large-chain
  append-and-verify test and gains a deterministic hot-cache assertion, with
  no pass/fail decision based on elapsed wall-clock ratios.
* `go test -race -count=3 ./...` passes locally before another hosted run.

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

### Option 5: Retry CI or raise the receipt test's timing multiplier

* Good, because it may make the next run green without changing test shape.
* Bad, because any finite multiplier retains the same unattributable
  wall-clock comparison and only changes how often it fails.

### Option 6: Optimize the production receipt store based on the failed ratio

* Good, because production optimization would be appropriate if code or a
  controlled benchmark showed size-dependent work.
* Bad, because F5-F7 show no growing-file scan or rewrite in the timed hot path,
  and one noisy ratio does not identify a production optimization target.

### Option 7: Replace the hard wall-clock ratio with deterministic large-chain integrity and hot-cache assertions

* Good, because each failure maps to a concrete functional or structural
  invariant.
* Good, because it preserves the original intent without depending on hosted
  runner filesystem and scheduler stability.
* Bad, because it detects cache and integrity regressions rather than measuring
  end-to-end latency; performance measurement becomes non-gating.

## More Information

The associated implementation plan is
[0111-PLAN-stabilize-asynchronous-receipt-test-teardown.md](0111-PLAN-stabilize-asynchronous-receipt-test-teardown.md).

The release workflow was not selected for modification: the same Go test step
is already correctly configured to run `go test -race ./...`, and the failure
occurred before tag-only build, artifact, and publishing steps.

### Implementation history

| Phase | Status | Evidence |
| --- | --- | --- |
| P1 — order asynchronous session receipt completion before cleanup | Implemented in `e4497c7`; confirmed for its target | Focused test passed 50 times locally with `-race`; `internal/session` passed hosted run 32549470400. |
| P2 — replace the receipt-store wall-clock gate | Implemented locally; awaiting hosted verification | New deterministic tests passed 20 focused race repetitions; `internal/receipt` passed under `-race`; `go test -race -count=3 ./...` passed. |
