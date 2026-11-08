---
title: "Go CLI Standards"
version: "1.26.5-v2"
last_updated: "2026-07-28"
component: "cli"
---

# Go CLI Standards

The `mcremote` command tree is implemented with Cobra in `internal/cli`. Its
root currently owns global `--config`, `--log-level`, and `--log-format` flags;
subcommands are `serve`, `version`, `pair`, `setup-service`, `engines`, `paths`,
`doctor`, `update`, `receipts`, and `auth-recovery` (`internal/cli/root.go`).

## Command design

- Keep `cmd/mcremote/main.go` and `cmd/mcrelay/main.go` to loading a detached
  update's handoff environment (MADR 0210 D2), execution, one user-facing error
  write, and process exit. The build identity is go-selfupdate-lib's
  `buildinfo`, stamped by the linker. The command package owns parsing and
  command behavior.
- Construct commands with `RunE`/`Args` validation and return errors to the
  single execution boundary. Keep `SilenceErrors` and `SilenceUsage` enabled on
  the root command so errors are not duplicated and usage is not emitted for
  operational failures.
- Long flags use `--name`; `-h` is help. The one short alias is `update`'s `-y`
  for `--yes`, which go-selfupdate-lib's update flags define. Do not add ad-hoc
  short aliases without a compatibility reason.
- Write stable, testable command examples and help text. Any behavior exposed
  by `--help` is part of the operator interface.

## Flags and configuration

Add a flag only when it has a well-defined precedence and owner. Bind ordinary
configuration flags through `config.Load`'s pflag mapping so the effective
order remains **flags > environment > YAML file > defaults**. `--tls` is a
documented exception: `serve` applies it after loading so TLS mode and enabled
state are reconciled together.

Never print credentials, device tokens, relay secrets, or certificate private
keys in command output, help, examples, errors, or logs.
