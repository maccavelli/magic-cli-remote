---
title: "Go Configuration Standards"
version: "1.26.5-v2"
last_updated: "2026-07-28"
component: "config"
---

# Go Configuration Standards

`internal/config` uses a fresh Viper instance for each load and YAML config
files. Do not introduce package-global Viper state.

## Precedence and discovery

The effective order is:

1. Bound CLI flags.
2. `MCREMOTE_*` environment variables.
3. An explicit file from `--config` or `MCREMOTE_CONFIG`, otherwise the XDG
   config-home `config.yaml`.
4. `config.Defaults()`.

An explicit file that cannot be read is an error. The implicit XDG file may be
absent. This difference prevents an operator typo from silently starting a
defaults-only daemon.

## Change rules

- Add each supported nested environment key explicitly with `BindEnv` and, when
  needed for Viper to discover it, `SetDefault`. Test the environment spelling.
- Keep config structs typed, validate after unmarshalling, and normalize coupled
  state once at the boundary. TLS mode is normalized after validation so every
  consumer receives a concrete mode.
- Preserve caller-provided values. Apply defaults field by field only when a
  zero value is actually unspecified; do not overwrite a complete struct with
  a global default.
- Treat configuration as sensitive input. Validate bounds, paths, hosts, and
  provider binaries before use, and avoid logging secrets.
- Update `configs/`, CLI help, config tests, and user documentation in the same
  change as a public setting.
