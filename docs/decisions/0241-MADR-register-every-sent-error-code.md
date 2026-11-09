---
status: accepted
date: 2026-11-08
decision-makers: Project Owner
consulted: "internal/protocol/errors.go and doc_coverage_test.go; internal/ws/error_codes_test.go, server.go, codex_execution.go; 0036-MADR (D4, D6); 0201-MADR (D10); the 0239 audit's P5 finding; read-only scratch scripts errreg_gap.py and emit_args.py"
informed: anyone adding an error code to the daemon, or writing a client that branches on one
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Register every error code the daemon sends, and close the registry guard's blind spots

## Context and Problem Statement

[0036-MADR-protocol-contract-completeness.md](0036-MADR-protocol-contract-completeness.md) D4 made the error codes a
registry. Every code is a constant in `internal/protocol/errors.go`, `protocol.ErrorCodes()` lists them, and two
tests close the loop:

* `TestWSErrorCodesAreRegistered` (`internal/ws/error_codes_test.go`) walks `internal/ws` with `go/ast` and fails on
  an emitted code the registry does not list;
* `TestErrorCodesAreDocumented` (`internal/protocol/doc_coverage_test.go`) fails on a registered code that
  `docs/guides/protocol-v1.md` does not mention.

The header of `errors.go` says what that buys: "Adding a code therefore fails the build until it is both registered
and documented." The 0239 audit found a code that is sent but not registered, which that promise rules out. This
record asks why, and how to make the promise hold.

### F1. `session_cancel_timeout` is sent and not registered [observed]

* `internal/protocol/errors.go:49` declares `ErrSessionCancelTimeout = "session_cancel_timeout"`.
* `internal/ws/server.go:2126` sends it: `s.writeError(ctx, c, env.ID, protocol.ErrSessionCancelTimeout, …)`, when
  `session.cancel` outlives its 10 s bound
  ([0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md](0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md)
  D10). `internal/ws/cancel_bound_test.go:116` asserts the frame.
* `protocol.ErrorCodes()` does not list it. Of the 89 `Err*` string constants in `errors.go`, it is the only one
  missing (`errreg_gap.py`).
* `protocol-v1.md:1044` documents it, so a reader of the guide is not misled. The registry is what is wrong.
* It came in with `589ba066` (2026-10-18), 0201's fix.

### F2. `not_found` is sent, not registered and not documented [observed]

* `internal/ws/codex_execution.go:131-146`, `executionErrorCode`, maps `provider.ErrTerminalNotFound` to the literal
  `"not_found"`. Four emit sites pass its result as the code (`codex_execution.go:158, 164, 173, 265`), so
  `codex.execution.read` and `codex.execution.write` answer `not_found` for a terminal that does not exist.
* No constant has that value, `ErrorCodes()` does not list it, and neither protocol guide mentions it.
* It came in with `fbee64f8` (2026-08-25), the codex execution surface.
* The phone does not branch on it (`apps/mobile/lib` has no `not_found`). Its value is part of the wire contract
  anyway: `errors.go`'s header says a code is renamed only with a protocol version bump.

### F3. Why the guard missed both: it sees only string literals in two places [observed]

0036 D4 chose an AST walk that "recognises two shapes, which between them cover all 71 emit sites": a string literal
at argument index 3 of `writeError` / `writePairError` / `writeSessionErr`, and a string literal assigned to an
identifier named `code`. It added that "call sites may adopt the constants opportunistically; the guard does not
depend on them doing so." Call sites did adopt them, and the guard does not see them. Today, in `internal/ws`'s
production files (`emit_args.py`):

| The code argument is | Emit sites | Seen by the guard |
| :--- | ---: | :--- |
| a string literal | 161 | yes |
| a `protocol.Err*` constant | 42 | **no** |
| a variable or a helper's result | 14 | only if a `code = "…"` literal feeds it |

Three blind spots follow:

* **A constant at an emit site.** The guard skips any argument that is not a literal. F1 is this case.
* **A constant assigned to `code`.** Seven sites (`code = protocol.Err…`) are skipped the same way.
* **A code-returning helper's `return`.** `executionErrorCode`, `authErrCode` and `workspaceErrCode` return codes
  that their callers pass on. A `return "x"` is neither an emit-site argument nor a `code =` assignment. F2 is this
  case. Besides `not_found`, the helpers return two literals today, `"bad_payload"` and `"unsupported"`, both
  registered. Everything else they return is a `protocol.Err*` constant.

`TestErrorCodesAreDocumented` checks only what the registry lists, so a code missing from the registry is missing from
both checks.

### F4. Nothing else is affected [observed]

* `ErrorCodes()` is read only by tests: the two above, and three loops in `internal/ws/workspace_handlers_test.go`
  (lines 194, 378, 463) that check codes they produce are registered. Adding entries changes no runtime behaviour.
* Every other `protocol.Err*` referenced in `internal/ws` is registered; the one other match, `ErrorPayload`, is a
  type.

## Decision Drivers

* The registry's promise in `errors.go` should be true: a code the daemon sends is registered, and so documented.
* The guard should fail on the class of error, not on these two instances. A later code added the same way must fail
  CI.
* No wire change. A code's value is a client contract, and neither code is renamed.
* Small and mechanical: no rewrite of the 161 literal sites, and no runtime check in production.

## Considered Options

* A. Register the two codes, and nothing else.
* B. Register the two codes, and close the three blind spots with two tests.
* C. Check codes at runtime, at the one function every error frame goes through.
* D. Replace every literal with a constant of a named type, so that only declared codes compile.

## Decision Outcome

Chosen option: **B**, because it fixes both codes and makes the next one fail CI, with two small tests and no change
to the wire or to production code paths.

* **D1. Register `session_cancel_timeout`.** `ErrSessionCancelTimeout` joins `ErrorCodes()`, beside
  `ErrRetryNoResult` in the envelope group where it is declared. `protocol-v1.md` already documents it.
* **D2. Register `not_found` under a constant, with its value unchanged.** `errors.go` gains
  `ErrTerminalNotFound = "not_found"` in the codex execution group, with a comment naming the operations that send
  it. `ErrorCodes()` lists it. `executionErrorCode` returns the constant. `protocol-v1.md`'s error reference gains a
  row beside `outcome_unknown`: the named terminal does not exist (it ended, or the id is wrong); re-read the terminal
  list. The constant's name says what it means here, and the value stays `not_found` because renaming it would be a
  protocol change (Q2).
* **D3. Every declared code must be registered.** A new test in `internal/protocol`,
  `TestEveryDeclaredErrorCodeIsRegistered`, parses `errors.go` with `go/ast`. It collects every exported `Err*` string
  constant and fails on any `ErrorCodes()` does not return, naming the constant and its value. It has no list of its
  own to maintain. This closes the first two blind spots: an emit site or a `code =` assignment can only name a
  `protocol.Err*` constant that `errors.go` declares, and D3 makes that constant registered. The test also fails if it
  finds fewer than 80 constants, so a moved or renamed file cannot make it pass vacuously.
* **D4. The guard reads the code-returning helpers' returns.** `TestWSErrorCodesAreRegistered` gains a third shape: in
  a function named in a new `errorCodeHelpers` set (`executionErrorCode`, `authErrCode`, `workspaceErrCode`), the first
  result of every `return` statement, when it is a string literal, must be registered. `TestErrorCodeEmittersStillExist`
  also checks that each helper in the set is still declared, as it does for the emitters. A new helper has to join the
  set; its doc comment says so.
* **D5. The 0239 report's finding** gains a dated note: resolved by this record.
* **Not changed:** the wire values, the 161 literal sites, the phone, and `protocol-v2.md`, which documents the codex
  surface's behaviour and leaves its codes to `protocol-v1.md`'s reference.

### Consequences

* Good, because both codes are registered, and `not_found` is documented for the first time.
* Good, because a code declared in `errors.go` and left out of `ErrorCodes()` fails CI with the constant's name.
* Good, because the helpers that compute codes are inside the guard.
* Neutral, because `not_found` keeps a generic value. Its constant name narrows what it means today.
* Bad, because a code passed through a variable that no `code = "…"` literal, helper `return` or declared constant
  feeds would still be invisible. No such site exists today (F3's table), and D4's set must be kept current by hand.

### Confirmation

1. `TestEveryDeclaredErrorCodeIsRegistered` fails on today's `errors.go` naming `ErrSessionCancelTimeout`, and passes
   after D1.
2. The extended `TestWSErrorCodesAreRegistered` fails on today's `codex_execution.go` naming `not_found`, and passes
   after D2.
3. Each new check is also seen failing on a scratch clone with one planted break: an `Err*` constant left out of
   `ErrorCodes()`; an unregistered literal returned by a helper; a helper removed from the package.
4. `go test ./internal/protocol/ ./internal/ws/`, `make race`, `make pre-add-check` on the changed Go files,
   `make check-records`, `make markdownlint-docs`, and CI on the push pass.
5. `TestErrorCodesAreDocumented` passes with `not_found` in `protocol-v1.md`.

## Pros and Cons of the Options

### A. Register the two codes, and nothing else

* Good, because it is a three-line change.
* Bad, because the blind spots stay. 42 constant emit sites, seven `code =` assignments and three helpers remain
  unchecked, which is how both codes got through.

### B. Register the two codes, and close the blind spots with two tests (chosen)

* Good, because D3 needs no list of its own, and D4 is one more shape in the existing walk.
* Good, because the failure names the constant or the helper and its line.
* Bad, because D4's helper set is maintained by hand. `TestErrorCodeEmittersStillExist` catches a removed helper, not
  a new one that was never added.

### C. Check codes at runtime

* Good, because it sees every code that is sent, however it was computed.
* Bad, because it sees only the paths tests exercise. `not_found` needs a missing codex terminal to fire.
* Bad, because it puts a check, and a choice of what to do on failure, into the production frame path.

### D. A named type for codes, with constants only

* Good, because it looks like a compile-time guarantee.
* Bad, because Go assigns an untyped string literal to a named string type, so `writeError(…, "anything", …)` would
  still compile. The guarantee is not real without the AST walk.
* Bad, because it touches all 217 emit sites for nothing D3 and D4 do not already give.

## More Information

### Owner questions, answered 2026-11-08

* **Q1. Scope.** The owner asked for `session_cancel_timeout`. The investigation found `not_found` (F2), sent the
  same way since 2026-08-25. Fix both, or only `session_cancel_timeout`? **Answer: "Fix both".** D2 stands.
* **Q2. `not_found`'s value.** Keep `not_found` on the wire with the constant `ErrTerminalNotFound`, or rename the
  value to something specific such as `terminal_not_found`, which `errors.go`'s rule makes a protocol version bump?
  **Answer: "Keep `not_found`".** D2 stands.

### Related

* [0036-MADR-protocol-contract-completeness.md](0036-MADR-protocol-contract-completeness.md): D4 (the registry and
  the AST walk), D6 (the documentation guard).
* [0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md](0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md):
  D10, which added `session_cancel_timeout`.
* [0239-REPORT-record-status-and-docs-accuracy-audit.md](../reports/0239-REPORT-record-status-and-docs-accuracy-audit.md):
  P5's finding.
* The plan: [0241-PLAN-register-every-sent-error-code.md](0241-PLAN-register-every-sent-error-code.md).
