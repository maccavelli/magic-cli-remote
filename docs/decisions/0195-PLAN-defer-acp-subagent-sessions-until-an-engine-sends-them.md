---
status: complete
date: 2026-10-16
associated-madr: "0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement deferring ACP subagent sessions, with a live test that pins the premise

Associated MADR: [0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md](0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md)

**Approved 2026-10-16** ("committed, proceed", after `1c42f09a`). This PLAN implements the MADR's
chosen option (S1–S5). U1 and U2 are executed; see the execution record.

* The owner approves the PLAN by name.
* Each phase ends with `make pre-add-check` on its staged Go files.
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

Magic keeps declining ACP's subagent variants and does not advertise `subagents`. A live test fails
the day the installed grok contains `subagent_update`, which is this record's trigger. 0184's D10
list records item 7 as deferred.

## Scope

* A new file, `internal/provider/grok/live_subagentsessions_test.go` (`live_grok` tag).
* One new example in `internal/provider/acpagent/advertise_test.go`'s `TestCapabilityHonesty`.
* 0184's MADR and PLAN: item 7's status.
* **Out of scope:** any product code, the phone, the vendor-channel handler, and the SDK gate gap.

## Implementation Steps

### U1. The live test (S2)

1. Add `TestLiveGrokShipsNoSubagentSessions`:
   * Resolve `grok` with `exec.LookPath` and `filepath.EvalSymlinks`, and skip if it is not on
     `PATH`.
   * Read the file and count `subagent_update`. Log the path, the size and the count.
   * Fail if the count is not zero. The message names the file and points at this MADR's S3.
   * Start no process and spend no tokens.
2. Show it passing against grok 1.0.50.
3. On a scratch copy, point it at a planted file containing `subagent_update`, through a temporary
   `PATH` directory holding a `grok` that is that file. Show it failing, naming the file.

4. Add `"subagents": {Subagents: &acp.SubagentCapabilities{}}` to `TestCapabilityHonesty`'s
   `dishonest` map (`internal/provider/acpagent/advertise_test.go:59-67`), which pins S1. Today no
   example names it. On a scratch copy with `"subagents": true` added to
   `implementedClientCapabilities`, show the test failing on that example.

### U2. Records (S1, S3–S5)

1. 0184's MADR: under D10, an amendment saying item 7 is deferred by this record, with its trigger.
2. 0184's PLAN: item 7's line points here.
3. This PLAN's execution record.

## Verification

* `go vet -tags live_grok ./internal/provider/grok/` and the pinned staticcheck with
  `-tags live_grok`.
* `go test -tags live_grok -run TestLiveGrokShipsNoSubagentSessions ./internal/provider/grok/`
  passes, and fails on the planted file.
* The existing guards are re-run: `go test -run
  'TestEverySessionUpdateIsMappedOrDeclined|TestCapabilityHonesty|TestInitializeFrameIsPinned'
  ./internal/provider/acpagent/`.
* `make pre-add-check`, `make lint`, `check_records.py --check-all`, `make markdownlint-docs`.
* `make ci-windows` on the Windows test host, for the `advertise_test.go` change.

## Rollout and Rollback

* A test and records only. Rollback is reverting the commit.

## Execution record

Executed 2026-10-16. No product code changed, and no deviation from the steps as written.

### U1. Tests

* `internal/provider/grok/live_subagentsessions_test.go`: `TestLiveGrokShipsNoSubagentSessions`, as
  steps 1.1–1.3 describe. Against the installed grok:

  ```text
  live_subagentsessions_test.go:39: grok-1.0.50: 160 MiB, subagent_update appears 0 time(s)
  --- PASS: TestLiveGrokShipsNoSubagentSessions (0.05s)
  ```

* `TestCapabilityHonesty` gained the `subagents` example, with a comment naming S1. It passes on
  the tree, as do `TestEverySessionUpdateIsMappedOrDeclined` and `TestInitializeFrameIsPinned`.
* Planted breaks on a scratch copy of the working tree:
  * A `grok` file holding the bytes `{"sessionUpdate":"subagent_update"}`, first on `PATH`:

    ```text
    live_subagentsessions_test.go:39: grok: 0 MiB, subagent_update appears 1 time(s)
    live_subagentsessions_test.go:41: <scratch>/u1fakebin/grok contains subagent_update 1 time(s): grok may now send ACP subagent sessions. Reopen 0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md (S3)
    --- FAIL: TestLiveGrokShipsNoSubagentSessions (0.00s)
    ```

  * `"subagents": true` added to `implementedClientCapabilities`:

    ```text
    --- FAIL: TestCapabilityHonesty (0.00s)
        advertise_test.go:72: subagents: advertised without behavior, and the check accepted it
    ```

### U2. Records

* 0184's MADR gained the amendment "D10 item 7 is deferred". Its PLAN's "A6 and later" section
  names 0187–0192 and this record.
* The MADR is `accepted`.

### Verification

```text
go vet -tags live_grok ./internal/provider/grok/ ./internal/provider/acpagent/   -> exit 0
staticcheck v0.8.2 -tags live_grok (same packages)                               -> exit 0
go test -race -count=1 ./internal/provider/acpagent/                             -> ok
make pre-add-check (advertise_test.go, live_subagentsessions_test.go)            -> 2 file(s) clean
make lint, make staticcheck                                                      -> exit 0
make ci-windows (the Windows test host, working tree)                            -> ci-windows-exit=0
check_records.py --check-all, make markdownlint-docs                             -> exit 0
```
