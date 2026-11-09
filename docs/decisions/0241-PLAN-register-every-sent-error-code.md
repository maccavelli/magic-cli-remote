---
status: in-progress
date: 2026-11-08
associated-madr: "0241-MADR-register-every-sent-error-code.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement registering every error code the daemon sends, and closing the registry guard's blind spots

Associated MADR: [0241-MADR-register-every-sent-error-code.md](0241-MADR-register-every-sent-error-code.md)

## Goal

When this plan is done:

* `session_cancel_timeout` and `not_found` are in `protocol.ErrorCodes()`, and `not_found` is documented (MADR D1,
  D2);
* a code declared in `errors.go` but not registered fails CI (D3);
* a code returned by `executionErrorCode`, `authErrCode` or `workspaceErrCode` that is not registered fails CI (D4);
* the 0239 report's finding points here (D5).

## Scope

* **Go:** `internal/protocol/errors.go`; `internal/protocol/errors_registry_test.go` (new);
  `internal/ws/codex_execution.go`; `internal/ws/error_codes_test.go`.
* **Docs:** `docs/guides/protocol-v1.md` (one table row).
* **Records:** this pair; `docs/reports/0239-REPORT-record-status-and-docs-accuracy-audit.md` (a dated note);
  `docs/README.md` (the index).
* **Not in scope:** the wire values; any other emit site; the phone; `protocol-v2.md`.

## Conventions

* **C1. Start state.** Each phase starts on a clean `master` equal to `origin/master`, or names what differs.
* **C2. Seen failing first.** Each new or extended check runs against today's code before the fix and fails naming
  F1's or F2's code. Planted breaks run in a scratch clone of HEAD, never in the tree.
* **C3. Go gates** before staging: `gofmt` and `golint` through `make pre-add-check FILES="…"` on every changed `.go`
  file, then `go test -count=1 ./internal/protocol/ ./internal/ws/`, then `make race`, with output redirected to the
  scratchpad and the exit status read before any filter.
* **C4. Staging.** The agent stages; the owner commits with `git commit --no-edit`. The disclosure guard's redact
  preview runs before each staging.
* **C5. Deviations.** Anything that does not match this plan or the MADR stops for the owner, and is recorded here
  with a date before work continues.

## Implementation Steps

### P0 — The records

1. This pair, `proposed`, with the owner's answers to Q1 and Q2 recorded in the MADR when given.
2. `check_records.py --write-index`; `make check-records`; the redact preview. **Stage.** P1 starts on the owner's
   word.

### P1 — The checks, seen failing on today's code (MADR D3, D4)

1. **`internal/protocol/errors_registry_test.go`,** package `protocol_test`:
   * parse `errors.go` (located beside the test) with `go/parser`;
   * collect every exported identifier beginning `Err` in a `const` declaration whose value is a string literal,
     with its unquoted value;
   * fail if fewer than 80 are found ("the guard would pass vacuously");
   * fail, per constant, on a value `protocol.ErrorCodes()` does not return: "`ErrX` (\"x\") is declared in errors.go
     but not in ErrorCodes(): register it and document it in docs/guides/protocol-v1.md (MADR 0241 D3)".
2. **`internal/ws/error_codes_test.go`:**
   * a package-level `errorCodeHelpers` set: `executionErrorCode`, `authErrCode`, `workspaceErrCode`, with a comment
     saying a new code-returning helper must join it;
   * in `TestWSErrorCodesAreRegistered`, for each `*ast.FuncDecl` whose name is in the set, visit its `*ast.ReturnStmt`
     nodes and pass the first result to `note` when it is a string literal. Closures inside the function are skipped,
     since their returns are not the helper's;
   * in `TestErrorCodeEmittersStillExist`, fail for each helper in the set that the package no longer declares, with
     the same wording as for the emitters;
   * the doc comment of `TestWSErrorCodesAreRegistered` names the third shape and MADR 0241.
3. **Seen failing on today's code** (C2): `go test -count=1 -run 'TestEveryDeclaredErrorCodeIsRegistered' ./internal/protocol/`
   fails naming `ErrSessionCancelTimeout` and nothing else; `go test -count=1 -run 'TestWSErrorCodesAreRegistered' ./internal/ws/`
   fails naming `not_found at …codex_execution.go:138` and nothing else. Logs go to the scratchpad. Any other
   failure is a deviation (C5).
4. P1 is not staged on its own, so `master` never carries a failing test. P1 and P2 are staged together.

### P2 — The registry and the doc (MADR D1, D2)

1. **`errors.go`:**
   * `ErrSessionCancelTimeout` joins `ErrorCodes()` after `ErrRetryNoResult`;
   * `ErrTerminalNotFound = "not_found"` is declared after `ErrCodexExecutionWriteFailed`, with a comment: the
     terminal a `codex.execution.read` or `codex.execution.write` names does not exist; the value is generic for
     historical reasons and changes only with a protocol version bump (MADR 0241 D2);
   * `ErrTerminalNotFound` joins `ErrorCodes()` after `ErrCodexExecutionWriteFailed`.
2. **`codex_execution.go:138`:** `return "not_found"` becomes `return protocol.ErrTerminalNotFound`.
3. **`protocol-v1.md`:** after the `outcome_unknown` row (line 1077), a row: `not_found` — the terminal a Codex
   execution request names does not exist: it ended, or the id is wrong. Re-read the terminal list
   (`codex.execution.read` action `terminals`) rather than retrying.
4. **Checks:**
   * both P1 tests pass; `TestErrorCodesAreDocumented` passes;
   * **planted breaks** (C2), a script in the scratchpad, on a scratch clone of HEAD with the working tree's changed
     files copied in, each break restored after its run:

     | Break | Expected failure |
     | :--- | :--- |
     | `ErrTerminalNotFound` removed from `ErrorCodes()` | D3 names `ErrTerminalNotFound` |
     | a new `ErrZz0241 = "zz_0241"` declared, not registered | D3 names `ErrZz0241` |
     | `workspaceErrCode` returns `"zz_0241"` in one branch | D4 names `zz_0241` and the line |
     | `errorCodeHelpers` gains `noSuchHelper` | the still-exists test names `noSuchHelper` |

   * C3's Go gates; `make check-records`; `make markdownlint-docs`; the redact preview.
5. **Stage P1 and P2 together**, with this plan's execution record.

### P3 — Close-out

1. The 0239 report's P5 finding on `session_cancel_timeout`, and the matching row of its P7 table, gain a dated note:
   resolved by this record, with the commit.
2. The MADR's Confirmation 1 to 5, each with its evidence, in the execution record; CI on the push.
3. This plan `complete`, the MADR `accepted`; `check_records.py --write-index`; `make check-records`. **Stage.**

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 D3 fails on today's code, passes after D1 | P1, P2 | P1 step 3; P2 step 4 |
| 2 D4 fails on today's code, passes after D2 | P1, P2 | P1 step 3; P2 step 4 |
| 3 planted breaks | P2 | step 4's table |
| 4 tests, race, pre-add, records, lint, CI | P2, P3 | C3; CI on the push |
| 5 `not_found` documented | P2 | `TestErrorCodesAreDocumented` |

## Rollout and Rollback

* **Rollout:** no wire change. The daemon sends exactly what it sends today; only the registry, two tests and one
  guide row change. It ships with the next release.
* **Rollback:** revert the P1+P2 commit. The tests go with the registry change, so `master` stays green.
* **Risk:** low. `ErrorCodes()` is read only by tests (MADR F4).

## Execution record

The owner answered the MADR's Q1 ("Fix both") and Q2 ("Keep `not_found`") on 2026-11-08, and the MADR was accepted
on those answers.

### P0 — 2026-11-08: the records

* This pair, with the answers recorded in the MADR (Owner questions).
* `check_records.py --write-index`; `make check-records`: exit 0; the redact preview: 0 files.
* **Staged** for the owner's commit: this pair and `docs/README.md`. P1 starts on the owner's word.

The owner committed P0 and approved the plan: "committed, proceed".

### P1 and P2 — 2026-11-08: the checks, then the registry

Scripts and logs are in the session scratchpad (`0241-p1-d3-failfirst.log`, `0241-p1-d4-failfirst.log`,
`p2_0241_planted.py`, `0241-planted.log`, `0241-preadd.log`, `0241-race.log`).

* **Start state (C1).** Clean tree. `master` was one commit ahead of `origin/master`: the owner's P0 commit, not yet
  pushed.
* **P1, the checks,** as steps 1 and 2 say:
  * `internal/protocol/errors_registry_test.go`: `declaredErrorCodes` parses `errors.go` and collects the exported
    `Err*` string constants; `TestEveryDeclaredErrorCodeIsRegistered` fails below 80 of them, and on each one
    `ErrorCodes()` does not return;
  * `internal/ws/error_codes_test.go`: the `errorCodeHelpers` set; the walk reads the first result of each `return`
    in those functions, skipping closures; `TestErrorCodeEmittersStillExist` checks the set; the doc comment names
    the third shape and why constants are left to D3.
* **P1, seen failing on today's code,** exactly as step 3 expected, with no other failure:

  ```text
  errors_registry_test.go:79: ErrSessionCancelTimeout ("session_cancel_timeout") is declared in errors.go but not in ErrorCodes(): register it and document it in docs/guides/protocol-v1.md (MADR 0241 D3)
  error_codes_test.go:161: unregistered error code not_found at codex_execution.go:138:10 — add it to protocol.ErrorCodes() and document it in docs/guides/protocol-v1.md
  ```

  `TestErrorCodeEmittersStillExist` passed: all three helpers exist.
* **P2, the registry and the doc,** as steps 1 to 3 say: `ErrSessionCancelTimeout` registered after
  `ErrRetryNoResult`; `ErrTerminalNotFound = "not_found"` declared after `ErrCodexExecutionWriteFailed`, with its
  comment, and registered in the same place in `ErrorCodes()`; `executionErrorCode` returns the constant; one row in
  `protocol-v1.md` after `outcome_unknown`.
* **P2, checks:**
  * `go test -count=1 ./internal/protocol/ ./internal/ws/`: both ok, `TestErrorCodesAreDocumented` included.
  * **Planted breaks** (`p2_0241_planted.py`, a scratch clone of HEAD with the working tree's five changed files):
    unplanted, both packages exit 0. Then each break, exit 1, with its own message:

    | Break | Message |
    | :--- | :--- |
    | `ErrTerminalNotFound` removed from `ErrorCodes()` | `ErrTerminalNotFound ("not_found") is declared in errors.go but not in ErrorCodes()` |
    | `ErrZz0241 = "zz_0241"` declared, not registered | `ErrZz0241 ("zz_0241") is declared in errors.go but not in ErrorCodes()` |
    | `workspaceErrCode` returns `"zz_0241"` in one branch | `unregistered error code zz_0241 at server.go:3277:10` |
    | `errorCodeHelpers` names `noSuchHelper` | `errorCodeHelpers names "noSuchHelper" but this package declares no such function` |

    The clone was removed and checked gone.
  * `make pre-add-check` on the four changed Go files: "4 file(s) clean (gofmt, golint, govulncheck)".
  * `make race`: exit 0, 49 `ok` lines, no FAIL, panic or race line.
  * `make check-records markdownlint-docs`: exit 0.
  * The redact preview: 0 files.
* **Staged together**, so `master` never carries a failing test: the four Go files, `protocol-v1.md`, and this plan.
