---
title: "Go Standards"
version: "1.26.5-v3"
last_updated: "2026-07-28"
status: "active"
applies_to: ["cmd/mcremote", "cmd/mcrelay", "internal", "scripts/smoke-protocol"]
go_version: "1.26.5"
---

# Go Standards

These standards apply to the Go module `github.com/maccavelli/magic-cli-remote`.
It is a two-binary project: `mcremote` is the secure coding-agent control-plane
daemon, and `mcrelay` is its outbound relay. Keep code and documentation scoped
to those roles; this is not a general microservice template.

## Repository facts

- The module declares Go 1.26.5 in `go.mod`.
- Entrypoints are `cmd/mcremote/main.go` and `cmd/mcrelay/main.go`; application
  packages are private under `internal/`.
- The build uses `CGO_ENABLED=0`, `-trimpath`, `-tags netgo,osusergo`, and
  `-ldflags '-s -w'` by default. These are release-build defaults, not a rule
  for every Go project.
- The CLI is Cobra/pflag; configuration is Viper over YAML, environment, and
  bound flags; the WebSocket server uses `github.com/coder/websocket`.
- The logging implementation is `log/slog`, with `zap` present only as a
  direct dependency—not an alternative project logging API.

## Required checks

Run the narrowest relevant command while developing, and `make preflight`
before an acceptance handoff when Go or mobile code changes. The repository's
authoritative commands are in the Makefile and `AGENTS.md`:

| Purpose | Command |
| --- | --- |
| Format Go | `make fmt` |
| Pre-add Go gate | `make pre-add-check FILES="a.go b.go"` |
| Unit tests | `make test` |
| Race suite | `make race` |
| Full local CI parity | `make preflight` |

The pre-add gate requires `gofmt`, `golint`, and `govulncheck` to pass for Go
files. Do not substitute `gofumpt` or `golangci-lint`: neither is configured by
this repository. `staticcheck` and `go vet` are part of `make preflight`.

## Documents

- [Core and build](core.md)
- [CLI](cli.md)
- [Configuration](config.md)
- [Networking](network.md)
- [Sessions](session.md)
- [Concurrency](concurrency.md)
- [Logging and errors](logging.md)
- [Testing](testing.md)

## External guidance

Use the repository conventions above first, then the official Go guidance on
[code review comments](https://go.dev/wiki/CodeReviewComments) and
[doc comments](https://go.dev/doc/comment). In particular, format with
`gofmt`, pass contexts explicitly, make goroutine lifetimes clear, and write
useful documentation for exported APIs.
