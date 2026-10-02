---
title: "Go Testing Standards"
version: "1.26.5-v3"
last_updated: "2026-07-28"
component: "testing"
---

# Go Testing Standards

The repository has broad package-level unit, protocol, persistence, network,
and race coverage. Add the smallest test that proves the behavior and the
regression it prevents; table-driven tests are useful for input/output matrices,
but are not mandatory when a focused scenario is clearer.

## Requirements

- Use `t.Run` with descriptive names for independent cases. Call `t.Helper()`
  in test helpers and fail at the call site.
- Test both acceptance and rejection paths for security, protocol, bounds,
  persistence, and cancellation changes.
- Avoid wall-clock sleeps and external network calls in ordinary tests. Inject
  clocks, transports, processes, or fakes where the package boundary allows.
  For concurrent logic involving timers or channels, consider `testing/synctest`
  to test event timing deterministically using virtualized time.
- Run `make test` for ordinary Go changes and `make race` for concurrent,
  session, relay, or WebSocket changes. `make preflight` runs formatting, tidy
  verification, vet, staticcheck, race tests, and mobile checks.
- Tests tagged `live_grok` and `live_opencode` invoke real CLIs/models. Keep
  them opt-in and run them at acceptance, not repeatedly in the edit loop.
- Use standard `//go:build` constraints for genuinely separate live or
  integration suites, with a blank line before `package`.

Before staging Go files, run the repository's `make pre-add-check`; it is the
enforced gate for `gofmt`, `golint`, and `govulncheck`.
