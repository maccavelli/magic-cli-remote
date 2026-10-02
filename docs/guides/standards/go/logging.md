---
title: "Go Logging and Error Standards"
version: "1.26.5-v2"
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
- Do not use `panic` for request, configuration, network, or provider failures.
