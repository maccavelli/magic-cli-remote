---
title: "Go Core and Build Standards"
version: "1.26.5-v3"
last_updated: "2026-07-28"
component: "core"
---

# Go Core and Build Standards

## Layout and API design

- Put binary-only wiring in `cmd/<binary>/main.go`; put reusable application
  behavior in focused `internal/<package>` packages. The existing commands are
  intentionally thin.
- Use short, lower-case, single-word package names. Avoid generic catch-all
  packages such as `util`, `common`, or `types`.
- Prefer concrete types at construction boundaries. Introduce interfaces where
  the consumer needs an abstraction, especially for a test seam—not as a
  speculative layer.
- Export only stable cross-package APIs. Document exported names with complete
  sentences beginning with the identifier's name.

## Build policy

`make build`, `make build-remote`, and `make build-relay` produce the release
artifacts. They default to a pure-Go, static-oriented build:

```make
CGO_ENABLED=0
GO_BUILDFLAGS=-trimpath -tags netgo,osusergo
GO_LDFLAGS=-s -w
```

The Makefile also link-stamps `main.version`, `main.commit`, and `main.date`.
Preserve those variables in the command `main` packages when changing version
reporting. Do not add `-gcflags='-N -l'` to release builds; it disables normal
compiler optimization.

`CGO_ENABLED=0` is a project default, not an absolute prohibition. A change
that genuinely needs cgo must document its portability, cross-compilation, DNS,
and user-lookup consequences and update the build path deliberately.

## Idiomatic Go (Go 1.22 – 1.26+)

- Let `gofmt` decide layout. Do not introduce a competing formatter rule.
- Prefer clear error flow with early returns. Error strings are lower-case and
  have no trailing punctuation because callers add context.
- Use `errors.Is` / `errors.As` with wrapping when callers need to classify an
  error. Reserve sentinel errors for stable, actionable conditions.
- **Slice & Map Standard Utilities**: Use standard library `slices`, `maps`, and
  `cmp` packages (`slices.Contains`, `slices.Delete`, `slices.SortFunc`, `maps.Clone`,
  `maps.Copy`) instead of writing custom iteration loops for common operations.
- **Iterators & Ranges**: Use integer range syntax (`for i := range count`) for counted
  loops and standard iterators (`iter.Seq`, `iter.Seq2`) when exposing custom collection
  traversals or streaming sequences.
- **Randomness**: Use `crypto/rand` for tokens, keys, pairing material, and other security
  values; never `math/rand`. For non-cryptographic PRNG needs, use `math/rand/v2`.
- **JSON Tags**: Use `omitzero` in `encoding/json` struct tags when zero values (e.g. empty
  structs, zero times) should be omitted without needing pointer redirection.
- **Filesystem Sandboxing**: Use `os.Root` for directory-scoped filesystem operations
  to enforce boundary containment and prevent path-traversal vulnerabilities.
- **Slices**: Choose `nil` slices when empty and nil are semantically equivalent. Use a
  non-nil empty slice only when the wire/API contract requires it (for example,
  JSON `[]` rather than `null`).

See [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) for the
underlying style guidance.
