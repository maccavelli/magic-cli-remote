---
title: "Go Standards"
version: "1.27.4-v1"
last_updated: "2026-11-18"
status: "active"
applies_to: ["cmd/mcremote", "cmd/mcrelay", "internal", "scripts/smoke-protocol"]
go_version: "1.27.4"
---

# Go Standards

These standards apply to the Go module `github.com/maccavelli/magic-cli-remote`.
It is a two-binary project: `mcremote` is the secure coding-agent control-plane
daemon, and `mcrelay` is its outbound relay. Keep code and documentation scoped
to those roles; this is not a general microservice template.

## Repository facts

- The module declares Go 1.27.4 in `go.mod`.
- Entrypoints are `cmd/mcremote/main.go` and `cmd/mcrelay/main.go`; application
  packages are private under `internal/`.
- The Makefile builds with `CGO_ENABLED=0`, `-trimpath` and `-ldflags '-s -w'`,
  plus `-tags netgo,osusergo` on every target except Darwin and Windows; `make verify-build-metadata`
  checks them. The published release binaries come from go-selfupdate-lib's
  build workflow, cgo-free with no tags. These are this project's defaults, not
  a rule for every Go project.
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
| Lint (golangci-lint, every build GOOS and tag) | `make lint` |
| Pre-add Go gate | `make pre-add-check FILES="a.go b.go"` |
| Unit tests | `make test` |
| Race suite | `make race` |
| Full local CI parity | `make preflight` |
| Library compatibility, before moving a pin (needs the network and a `../go-shared` checkout) | `make check-libs` |

The pre-add gate requires `gofmt`, `golangci-lint` (v2.15.0, configured by
`.golangci.yml` and run by `make lint`), and `govulncheck` to pass for Go files.
Do not substitute `gofumpt`: it is not configured by this repository.
`staticcheck`, `golangci-lint` and `go vet` are part of `make preflight`.

The one deprecated use this repository allows is an `acp-go-sdk2` name that is
deprecated only because it comes from the unstable ACP schema (MADR 0207,
amendment H). Each such use carries a pair, because `staticcheck` reads only
`//lint:ignore` and `golangci-lint` reads only `//nolint`: a
`//lint:ignore SA1019 <reason citing a record>` on the line above, and
`//nolint:staticcheck // as the lint:ignore above` at the end of the line it
covers. A file that needs the whole file covered has
`//nolint:staticcheck // as the lint:file-ignore below` above its `package`
clause and a `//lint:file-ignore SA1019 <reason>` beneath it. `make staticcheck`
runs `scripts/check_staticcheck_ignored.py`, which refuses a directive without
its pair and a count above the bound recorded in the script.

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
