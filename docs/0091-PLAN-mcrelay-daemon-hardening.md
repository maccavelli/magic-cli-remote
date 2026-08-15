---
status: draft
date: 2026-08-15
associated-madr: "0091-MADR-mcrelay-daemon-hardening.md"
owner: [Implementer]
target-milestone: [After 0091 review; P1 first]
---

<!-- markdownlint-disable MD013 MD024 -->

# Plan: Harden mcrelay's unit PATH, plaintext policy, and join-plane TLS surface

## Executive Summary & Goal

* **Associated Decision Record**: [0091-MADR-mcrelay-daemon-hardening.md](./0091-MADR-mcrelay-daemon-hardening.md)
* **Goal**: Implement 0091 D1–D3, D5, D10 immediately; run the D4 probe
  before touching `RestrictAddressFamilies` / `MemoryDenyWriteExecute`;
  leave D6–D9 as documented non-work.
* **Success Criteria**:
  * [ ] `mcrelay setup-service --print-only` PATH has no agent bins;
        `UMask=0077` present
  * [ ] `mcremote setup-service --print-only` PATH unchanged
  * [ ] `tls.mode=off` on non-loopback listen fails; loopback / 
        `--allow-plaintext` still works
  * [ ] Join-plane `tls.Config` used by mcrelay has no `h2`
  * [ ] `MaxHeaderBytes == 16<<10` with a test that oversized headers
        get `431` / connection close
  * [ ] `go test -race ./internal/relay/... ./internal/cli/service/...`
        and `make pre-add-check` on touched Go files

## Prerequisites & Dependencies

* **Infrastructure**: a host that can run `mcrelay serve` with the
  *current* unit plus a temporary drop-in for the D4 probe (this
  machine is enough).
* **Dependencies**: none. No module bumps. ACME `h2` change is local
  to how mcrelay consumes `certs.ACMEBundle.TLSConfig` (do **not**
  silently change mcremote's ACME helper if it still wants `h2`).
* **Pre-Flight Checks**:
  * [ ] Confirm `servicePathEnv` is shared (`internal/cli/service/setup.go`)
  * [ ] Confirm ACME `NextProtos` prepends `h2` (`internal/certs/acme.go`)
  * [ ] Confirm plaintext is warn-only (`internal/relay/server.go`
        `ListenAndServe` default branch)
  * [ ] 0091 accepted or D-items explicitly approved

## Architecture & Technical Design Summary

```
phone / mcremote host
        │  WSS (HTTP/1.1 upgrade)
        ▼
[ systemd user unit ]  PATH slim, UMask=0077
        │  ExecStart=mcrelay serve --config …
        ▼
[ cmd/mcrelay → relay.Server ]
   GET /healthz
   GET /v1/host     register (allowlist + constant-time)
   GET /v1/phone    join (host_id + rate/slot caps)
   GET /v1/tunnel   short-lived token
        │
        ▼
   opaque splice (unchanged)
```

D10 must strip `h2` on the **mcrelay** listener only. Prefer a wrapper
in `internal/relay/tls.go` after `ApplyTLS` (`cfg.TLSConfig.NextProtos`
without `h2`) over editing `certs.ACMEBundle.TLSConfig`, which other
callers may share.

D5 belongs in `ListenAndServe` (or `newServeCmd` after `ApplyTLS`):
if there is no `TLSConfig` and no cert files, require loopback
`listen.host` or `--allow-plaintext`.

D1: `servicePathEnv(home, product)` or `mcrelayPathEnv(home)`.

## Phased Implementation Plan

### Phase 1: Setup & Groundwork

* **Objective**: Split PATH, add UMask, pin tests so mcremote cannot
  regress.
* **Tasks**:
  - [ ] **Task 1.1**: `servicePathEnv` takes product; mcrelay gets
        `~/.local/bin:/usr/local/bin:/usr/bin:/bin` (plus
        `XDG_RUNTIME_DIR` still set). mcremote path unchanged.
  - [ ] **Task 1.2**: `UMask=0077` in `mcrelay.user.service.tmpl`,
        `deploy/systemd/mcrelay.user.service`,
        `deploy/systemd/mcrelay.service`.
  - [ ] **Task 1.3**: Extend `TestRenderUnitMcrelay` /
        `TestRenderUnit` for D1/D2. Document PATH in
        `docs/config-mcrelay.md` / `docs/ops-mcrelay.md`.

### Phase 2: Core Implementation

* **Objective**: Listener hygiene (D3, D5, D10).
* **Tasks**:
  - [ ] **Task 2.1**: `http.Server.MaxHeaderBytes = 16 << 10` in
        `relay.New`. Test with a handshake whose headers exceed 16 KiB.
  - [ ] **Task 2.2**: Fail closed on plaintext + non-loopback. Flag
        `--allow-plaintext`. Tests: `127.0.0.1` + off succeeds;
        `0.0.0.0` + off errors; flag overrides.
  - [ ] **Task 2.3**: After `ApplyTLS`, strip `h2` from
        `cfg.TLSConfig.NextProtos` (and from file-mode `tls.Config` if
        any default appears). Test `NextProtos` on the config
        `ListenAndServe` will use. Do not change mcremote ACME
        behaviour unless a shared helper is parameterized.

### Phase 3: Integration, Telemetry & Fallbacks

* **Objective**: D4 probe only. No production filter yet.
* **Tasks**:
  - [x] **Task 3.1**: Probe recorded 2026-08-15 on wonder (systemd
        `--user` transient `mcrelay-d4-probe.service`):
        `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` +
        `MemoryDenyWriteExecute=true` + `PrivateTmp=true`. Binary
        `CGO_ENABLED=0`. Listen `127.0.0.1:18443` TLS files.
        - `ss`: `LISTEN 127.0.0.1:18443`
        - `curl -sk https://127.0.0.1:18443/healthz` → `200 {"ok":true}`
        - `MCRELAY_D4_URL=wss://127.0.0.1:18443 go test -run TestD4LiveSandbox`
          → pass (register + join + splice echo)
        - journal: `listening addr=127.0.0.1:18443 tls=files` then
          `host registered host_id=d4-host` → `join ok` → `splice ended`
          `reason=client_gone`. No syscall denials.
  - [ ] **Task 3.2**: If the probe passes, add both directives to the
        three unit files with the same “disable in a drop-in” comment.
        If it fails, keep them omitted and write the failing syscall /
        errno here.
  - [ ] **Task 3.3**: Do **not** add `SystemCallFilter` (D6),
        phone join tokens (D8), `/metrics` (D9), or user-unit
        `ProtectHome` (D7).

### Phase 4: Verification, Migration & Cutover

* **Objective**: Install path and docs.
* **Tasks**:
  - [ ] **Task 4.1**: `go test -race ./internal/relay/...
        ./internal/cli/service/...`
  - [ ] **Task 4.2**: `make pre-add-check FILES="…"` then commit
        without `-m` if asked to land.
  - [ ] **Task 4.3**: Operators with an existing unit must re-run
        `mcrelay setup-service --force` (or copy the example) to pick
        up PATH/UMask. Document that in `ops-mcrelay.md`. Existing
        processes keep running until that rewrite + restart.

## Verification & Testing Strategy

| Test Level | Scope & Target | Execution Method | Passing Requirement |
| :--- | :--- | :--- | :--- |
| **Unit** | PATH/UMask render; MaxHeaderBytes; plaintext policy; NextProtos | `go test ./internal/cli/service ./internal/relay` | 100% pass |
| **Race** | relay + service | `go test -race ./internal/relay/... ./internal/cli/service/...` | 100% pass |
| **Probe** | D4 sandbox | live unit drop-in + one splice | bind + register + join succeed |
| **Regress** | 0016/0017 e2e (Origin, tokens, rate, healthz) | existing `TestE2E*` | no new failures |

## Rollback & Mitigation Procedures

* **Trigger**: mcrelay fails to start after unit rewrite; or TLS
  clients break after `h2` removal; or plaintext listeners in a
  known lab setup start exiting.
* **Rollback**:
  1. Unit: restore previous `~/.config/systemd/user/mcrelay.service`
     (or drop-in `Environment=PATH=` / `UMask=` override) and
     `systemctl --user daemon-reload && restart`.
  2. D5: pass `--allow-plaintext` or listen on `127.0.0.1`.
  3. D10: revert the NextProtos strip; ACME path works as before.
  4. D4: remove the two lines from the drop-in / template.
* No schema or on-disk format change. ACME cache layout unchanged.

## Task Progress Checklist

- [x] **Phase 1: Setup & Groundwork**
  - [x] Task 1.1: Product-specific PATH
  - [x] Task 1.2: `UMask=0077`
  - [x] Task 1.3: Render tests + docs
- [x] **Phase 2: Core Implementation**
  - [x] Task 2.1: MaxHeaderBytes
  - [x] Task 2.2: Plaintext fail-closed
  - [x] Task 2.3: Strip `h2` on mcrelay TLS
- [x] **Phase 3: Integration & Telemetry**
  - [x] Task 3.1: D4 live probe
  - [x] Task 3.2: Enable filters only if probe passes
  - [x] Task 3.3: Leave D6–D9 untouched
- [ ] **Phase 4: Migration & Cutover**
  - [ ] Task 4.1: Race tests
  - [ ] Task 4.2: pre-add-check
  - [ ] Task 4.3: ops note for `--force`
