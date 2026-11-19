---
title: "Go Logging and Error Standards"
version: "1.27.4-v2"
last_updated: "2026-07-28"
component: "logging"
---

# Go Logging and Error Standards

## Logging

Use `log/slog` for operational logs. Constructors accept an optional logger,
fall back to `slog.Default()`, and derive a component logger such as `ws`.

- Use stable keys (`component`, `session_id`, `device_id`, `provider`, `err`)
  and structured values. Do not concatenate operational fields into messages.
- Log an error at the boundary that can make the operational decision. Lower
  layers normally return a wrapped error; do not log it and return it again.
- Never log pair codes, bearer/device tokens, relay secrets, private keys,
  complete certificates, or raw user prompts without an explicit reviewed need.
- `fmt` and Cobra output are appropriate for user-facing command responses;
  they are not replacements for operational logging.

## Errors

- Wrap an error with `%w` when its identity matters to a caller. Add useful
  operation context, not a restatement of the callee name.
- Use package-level sentinel errors only for stable conditions callers need to
  branch on. Prefer typed errors when they carry structured remediation data.
- Handle errors explicitly. Ignoring an error is acceptable only for a clearly
  best-effort cleanup/write where failure cannot affect correctness and the
  reason is evident in nearby code.
- An ignored error is an explicit discard, `_ = f()` or `_, _ = f()`, never a
  bare call. `errcheck` (on in `make lint`, with `check-blank: false` here) reports
  the bare call and accepts the explicit discard, so each discard is a decision a
  reviewer reads: add a short comment when the reason is not evident from the
  line. No `//nolint:errcheck`, no `exclude-functions`, no path exclusion.
- By case. A file that was written is closed with its error checked and
  returned, unless an earlier error is already being returned, in which case
  `_ = f.Close()`. A handle that was only read, a response body, a socket or a
  directory is closed with `defer func() { _ = x.Close() }()`. A command's status
  lines go through `internal/cliout`, which drops the write error in one place,
  because the stream that failed is the only place to report it. What a command
  outputs as its result is not a status line and does not go through it: a
  generated file body, a JSON document, a statement of record, a value shown once
  (a pairing token or code), a result a script acts on (a status or verify line)
  is written with `fmt.Fprint*`, and a failed write is
  returned, wrapped with what was being written and never with the value itself,
  so the command exits non-zero. Where the command already fails with its own
  result, a broken chain say, the write error is added to that error and does not
  replace it. A receive from a channel that carries an error (a
  process's exit status, a serve result) is not left bare: `errcheck` rejects the
  bare receive and `staticcheck` S1005 rejects `_ = <-ch`, so the value is taken
  into a variable, and what is done with it is a choice: these sites log it at
  debug or warn level, and `err := <-ch; _ = err` would satisfy both linters too.
  A single-value type assertion is written with its ok. Where the function returns
  no error, as in a provider's constructor, a failed assertion panics with the
  runtime's text, `interface conversion: <interface> is <got>, not <want>`, the same for a non-nil value (a nil one reads
  `<nil>` where the runtime's reads `nil`).
- Do not use `panic` for request, configuration, network, or provider failures.
