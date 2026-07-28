---
title: "Go Concurrency and Lifecycle Standards"
version: "1.26.5-v3"
last_updated: "2026-07-28"
component: "concurrency"
---

# Go Concurrency and Lifecycle Standards

- Accept `context.Context` as the first parameter, conventionally named `ctx`,
  for request-scoped I/O, provider calls, and goroutine work. Do not store a
  context in a long-lived struct.
- Every goroutine needs an owner, cancellation path, and exit condition. Make
  these obvious in code and test them when a change introduces background work.
- Leverage Go 1.22+ per-iteration loop variable scope: loop variables captured by
  goroutines or closures inside `for` loops create distinct variables per iteration,
  preventing race conditions without manual variable shadow copying.
- Place each mutex beside a comment and the fields it guards. Snapshot shared
  state under the lock, then perform I/O, callbacks, channel sends, and socket
  closes after releasing it.
- Use channels for ownership transfer and notifications, not as an unbounded
  queue. Bound queues that receive peer-controlled or streaming data.
- Use `sync.Once` or equivalent idempotence only for one-time transitions such
  as closing a connection; do not use it to hide lifecycle ambiguity.
- Propagate shutdown from the server/process root. Listener shutdown must also
  cancel provider/session work and explicitly close hijacked WebSockets.

The Go review guidance recommends explicit context propagation and clear
[goroutine lifetimes](https://go.dev/wiki/CodeReviewComments#goroutine-lifetimes).
