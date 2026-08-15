---
status: proposed
date: 2026-08-15
decision-makers: [Project Owner]
consulted: [Implementer]
informed: [Operators running mcrelay as a public join-plane edge]
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Harden the mcrelay daemon and its systemd unit without widening the trust model

## Context and Problem Statement

`mcrelay` is the public join-plane edge: phones and `mcremote` hosts dial
it; it never terminates protocol-v1. The binary is `cmd/mcrelay` →
`internal/relay.Execute` / `Server`. The unit that `mcrelay setup-service`
writes is now
[`internal/cli/service/mcrelay.user.service.tmpl`](../internal/cli/service/mcrelay.user.service.tmpl)
(no longer the shared mcremote template). Example copies live in
[`deploy/systemd/mcrelay.user.service`](../deploy/systemd/mcrelay.user.service)
and [`deploy/systemd/mcrelay.service`](../deploy/systemd/mcrelay.service).

MADR [0015](0015-MADR-mcrelay-transport-security.md),
[0016](0016-MADR-mcrelay-audit-hardening.md), and
[0017](0017-MADR-mcrelay-memory-security-action-plan.md) already locked
the trust model and closed R1–R40 / A–D / E1–E3. This record is a
**2026-08-15 reassessment** of residual attack surface: process sandbox,
HTTP/TLS listener hygiene, and a few daemon knobs that 0016/0017 did
not cover. It is grounded in the current tree, in systemd and WebSocket
hardening practice, and in Go’s own release-build defaults.

The question is: **what additional hardening is still worth doing on the
mcrelay binary and the unit `setup-service` deploys, without reopening
0015’s trust model or repeating 0016/0017 work?**

Scope: `cmd/mcrelay`, `internal/relay`, `internal/certs` ACME TLS
config as consumed by mcrelay, `internal/cli/service` PATH + unit
templates, `deploy/systemd/mcrelay*.service`. Out of scope: mobile
`RelayTransport`, `internal/relayhost` (the host *client*), mcremote
listen.host, Prometheus (0017 E5, still deferred).

## Decision Drivers

* Fail closed on the public edge: never parse inner TLS; never mint
  mcremote sessions from a phone join alone (0015 D2).
* User-unit safe: no `ProtectClock` / `ProtectKernelModules` /
  `ProtectKernelLogs` / `ProtectHostname` on the systemd *user* manager
  (218/CAPABILITIES crash loop on container-restricted hosts).
* Config and ACME cache live under `$HOME` for the user unit — 
  `ProtectHome` / `ProtectSystem` would hide them.
* mcrelay does **not** exec coding CLIs (unlike mcremote), so
  `PrivateDevices` and `RestrictNamespaces` are in-bounds.
* Do not add surface (metrics, admin HTTP, pprof) to “harden” the edge.
* Probe-then-enable for seccomp / address-family / MDWE filters that
  can break `net.Listen` or the Go runtime.
* Keep operator disable path as a drop-in (`=false`), not by omitting
  keys from the template.

## Considered Options

* Option 1: Prioritized hardening tranche (P1 now, P2 after probes, P3 deferred)
* Option 2: Status quo — ship the current unit + 0016/0017 daemon as-is
* Option 3: Maximal sandbox immediately (DynamicUser, SystemCallFilter,
  ProtectHome, MDWE, TLS 1.3-only, join tokens) in one cut

## Decision Outcome

Chosen option: **"Prioritized hardening tranche"**, because the remaining
value is real but uneven: a few unit and listener fixes are cheap and
safe; seccomp / MDWE / address-family filters need a live probe; join
tokens and password KDFs would reopen 0015.

* Implementation Plan: [0091-PLAN-mcrelay-daemon-hardening.md](./0091-PLAN-mcrelay-daemon-hardening.md)

### Decisions locked for review

| # | Topic | Chosen |
|---|--------|--------|
| **D1** | setup-service `PATH` | **Product-specific.** mcrelay’s unit must not inherit grok/opencode/kilo/flutter/`~/go/bin`. Minimal: `~/.local/bin` + `/usr/local/bin` + `/usr/bin` + `/bin`. mcremote keeps the agent PATH. |
| **D2** | File-creation mask | **`UMask=0077`** on user and system units. ACME cache and any unit-created files default to owner-only. |
| **D3** | HTTP header DoS | Set `http.Server.MaxHeaderBytes` to **16 KiB** (stdlib default is 1 MiB). Join-plane upgrades need only a short handshake. |
| **D4** | Address families / MDWE | **Probe, then enable.** Candidate: `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` and `MemoryDenyWriteExecute=true` (binary is `CGO_ENABLED=0`). Land in templates only after `mcrelay serve` binds TLS, accepts a register+join, and completes a splice under those directives. |
| **D5** | Plaintext listen | **Fail closed unless loopback or explicit opt-in.** `tls.mode=off` (or auto-off) on a non-loopback `listen.host` is an error; `--allow-plaintext` (or equivalent) remains for tests. Today `ListenAndServe` only *warns* (`internal/relay/server.go`). |
| **D6** | `SystemCallFilter=` | **Not in the first ship.** `@system-service` is the systemd-recommended allow-list, but a missed syscall takes the public edge down. Schedule after D4 soaks. |
| **D7** | `ProtectHome` / `ProtectSystem` on the *user* unit | **Keep off.** Config + ACME cache are under `$HOME`. System unit may keep `ProtectSystem=strict`; `ProtectHome=true` only when `User=` + `StateDirectory=` / `/var/lib/mcrelay` are actually used. |
| **D8** | Phone `/v1/phone` join token | **Do not add.** Knowing `host_id` is enough to *attempt* a join; inner TLS + device token authenticate to mcremote after the splice (0015). Slot/rate caps stay the DoS control. |
| **D9** | Metrics / admin HTTP | **Keep 0017 E5 deferred.** No new listener, no `/metrics`, no pprof in the release binary (`make debug` / `debugpprof` only). |
| **D10** | HTTP/2 on the join listener | **Disable `h2` on the mcrelay TLS config.** ACME `TLSConfig()` currently prepends `h2` (`internal/certs/acme.go`). File-mode TLS does not. The join plane is HTTP/1.1 WebSocket upgrade; HTTP/2 is unused surface (HPACK / multiplexing). TLS-ALPN-01 is not the HTTP-01 path. |

## Consequences

* Good, because the unit no longer advertises a coding-agent `PATH` on a
  process that never `exec`s.
* Good, because plaintext-on-the-internet becomes a hard error instead
  of a log line operators can miss.
* Good, because header-bomb and HTTP/2 surface shrink without touching
  the splice path.
* Bad, because D4/D6 can fail on unusual kernels; they need a probe
  host and a drop-in escape hatch.
* Bad, because D5 breaks any operator who currently serves plaintext on
  `0.0.0.0` “for a minute” — they must pass an explicit flag.
* Neutral, because 0015/0016/0017 behaviour (opaque splice, Origin
  default, rate buckets, tunnel tokens, constant-time register) is
  unchanged.

## Pros and Cons of the Options

### Option 1: Prioritized hardening tranche (Chosen)

Ship the cheap, evidence-backed items; probe the filters that can
crash the edge; leave trust-model changes out.

* Good, because it matches how 0016/0017 already sequenced work
  (P1 first, probes, then optional).
* Good, because every D-item maps to a file and a test.
* Bad, because the public edge is not “maximally sandboxed” on day one.
* Neutral, because drop-ins still override any new `=true`.

### Option 2: Status quo

* Good, because 0016/0017 already put this edge in “adequate → strong”
  and the new mcrelay template already has the user-safe eight plus
  `PrivateDevices` / `RestrictNamespaces`.
* Bad, because `setup-service` still injects an agent `PATH` into a
  process that must not exec those binaries.
* Bad, because `tls.mode=off` on `0.0.0.0` is still a warning.
* Bad, because ACME mode still offers HTTP/2 on the join port.

### Option 3: Maximal sandbox in one cut

* Good, because `systemd-analyze security` score would jump.
* Bad, because `ProtectHome` on the user unit hides ACME cache.
* Bad, because `SystemCallFilter` / `RestrictAddressFamilies` without
  a probe is a boot-loop class of bug on a public edge.
* Bad, because a phone join-token reopens 0015 and the mobile client.

## Confirmation

* `mcrelay setup-service --print-only` unit contains D1 PATH (no
  `.grok/bin` / `.opencode/bin` / `.cache/kilo/bin` / `flutter`) and
  `UMask=0077`.
* `TestRenderUnitMcrelay` asserts those lines; mcremote render test
  still expects the agent PATH and must **not** gain `UMask` unless
  mcremote chooses it separately.
* `go test -race ./internal/relay/...` : MaxHeaderBytes reject,
  plaintext-on-non-loopback reject, ACME/file TLS config has no `h2`
  on the join `tls.Config` used by `ListenAndServe`.
* D4 probe log attached to the PLAN before those two directives land
  in the template (live `serve` + one register/join/splice).
* `systemd-analyze --user security mcrelay.service` after install:
  existing checks stay green; new ones only appear after D4.

## More Information

### A. Daemon — what the tree already does (do not redo)

Grounded in `internal/relay` as of this record.

| Control | Where | Notes |
|---------|-------|-------|
| Four HTTP routes only | `server.go` `New` | `GET /healthz`, `GET /v1/host`, `GET /v1/phone`, `GET /v1/tunnel`. No POST body API, no admin, no pprof in release. |
| Opaque splice | `handlePhone` / `splice` | No protocol-v1 parse (0015 D2). |
| Origin | `upgrade` → `websocket.Accept` with empty `AcceptOptions` | coder/websocket **rejects cross-origin browsers** by default; native clients with no `Origin` are accepted (0016 D5). CWE-1385 / CSWSH baseline. |
| Register auth | `hub.checkSecret` | SHA-256 then `subtle.ConstantTimeCompare` against real or dummy hash (0017 D12). Unknown host and wrong secret share `unauthorized`. |
| Tunnel auth | `hub.claimTunnel` | 32-byte `crypto/rand` token, hex, constant-time compare; legacy registration secret **off** unless `allow_legacy_tunnel_secret` (0017 D13). |
| Rate limits | `allowRateRetry` | Buckets: accept / join / register / join-per-host. Map cap 4096, TTL 2 min, background prune (0016 D6, 0017 E3). |
| ID validation | `validateHostID` / `validateSessionID` | Before rate keys (0017 D7). |
| Trusted proxies | `clientIP` | Default **RemoteAddr only**; XFF honoured only from `trusted_proxies` (0017 E1). |
| `/healthz` | `handleHealthz` | `{"ok":true}` only (0016 R11). |
| TLS floor | `ListenAndServe`, `certs` | `MinVersion = TLS 1.2`. File mode sets it locally; ACME bundle too. |
| Timeouts | `http.Server.ReadHeaderTimeout=10s`; `firstEnvelopeTimeout=10s`; TCP keepalive 25/5/4; splice idle 5m / max 12h | `WriteTimeout` / `IdleTimeout` unset — correct for hijacked WS. |
| Control vs splice caps | `ControlReadLimitBytes=64KiB` vs `MaxMessageBytes` default 1 MiB, clamp 16 MiB | 0017 D9 / D16. |
| Shutdown | `drainConnections` | Closes splices + host controls (0017 D11). |
| Build | `Makefile` | `CGO_ENABLED=0`, Linux `netgo,osusergo`, `-trimpath`, `-ldflags -s -w`. Release has no `debugpprof`. |
| Secrets in config | `FileConfig.Validate` | At least one host; secret min 16; unit file mode 0600 when `--env` carries secrets. |

SHA-256 of a **high-entropy ≥16-char** registration secret is appropriate
(it is not a user password). A KDF (argon2id) is **not** recommended
here: it would add CPU DoS on `/v1/host` and does not change the
threat if the allowlist file is 0600 and the secret is random.

### B. Daemon — residual findings (this pass)

IDs continue the **R** series.

| ID | P | Finding | Evidence | Disposition |
|----|---|---------|----------|-------------|
| **R41** | P2 | `tls.mode=off` (or auto-off) on a public bind is only a **warning** | `ListenAndServe` default branch; `newServeCmd` Long still documents `off` as “local tests” | **D5** fail closed |
| **R42** | P2 | `http.Server.MaxHeaderBytes` left at stdlib **1 MiB** | `New` sets only `ReadHeaderTimeout` | **D3** 16 KiB |
| **R43** | P2 | ACME join-plane `tls.Config` advertises **`h2`** | `internal/certs/acme.go` `TLSConfig` `NextProtos = append([]string{"h2", "http/1.1"}, …)`. File-mode TLS does not. Join plane is HTTP/1.1 upgrade. | **D10** drop `h2` for mcrelay |
| **R44** | P2 | `setup-service` PATH is the **mcremote agent PATH** | `servicePathEnv` always prepends `.grok/bin`, `.opencode/bin`, `.cache/kilo/bin`, `flutter`, `~/go/bin` | **D1** |
| **R45** | P3 | ACME HTTP-01 still binds **:80** as a second listener | `applyLetsEncrypt` / `certs.EnsureACMEHTTP` | Keep; document. Prefer DNS-01 when :80 is undesirable. Not a code defect. |
| **R46** | P3 | `ConstantTimeCompare` on tunnel tokens bails out on **length mismatch** | `claimTunnel`; tokens are always 64 hex chars | Accept. Length is not secret. Do not pad. |
| **R47** | P3 | No global max-connections beyond rate + `MaxHosts` / `MaxPhonesPerHost` / `MaxConcurrentJoin` | `hub` + `allowRateRetry` | Leave. Another counter does not beat the existing four. |
| **R48** | P3 | No HSTS on `/healthz` | `handleHealthz` | Optional one-liner if TLS is on; not required for a JSON liveness probe that browsers should not pin. Skip unless a browser client appears. |
| **R49** | P3 | User unit has no `UMask=` | `mcrelay.user.service.tmpl` | **D2** |
| **R50** | P3 | `SystemCallFilter` / `RestrictAddressFamilies` / `MDWE` absent | User template comments leave MDWE off “for Go plugins/cgo”; this binary is `CGO_ENABLED=0` | **D4** probe, **D6** later |

Not findings:

* Unauthenticated `/v1/phone` given a valid online `host_id` — **0015
  design**. Inner TLS + device token is the real auth. Rate/slot caps
  are the abuse control (**D8**).
* `/healthz` unauthenticated — intentional liveness (R11).
* No `WriteTimeout` — would kill long splices after hijack in some
  stdlib paths; do not set on this `http.Server`.

### C. Unit template evaluation

**User template** (`mcrelay.user.service.tmpl`) already has, on by
default: `NoNewPrivileges`, `PrivateTmp`, `RestrictSUIDSGID`,
`LockPersonality`, `RestrictRealtime`, `ProtectKernelTunables`,
`ProtectControlGroups`, `SystemCallArchitectures=native`,
`PrivateDevices`, `RestrictNamespaces`. `KillMode=mixed` is correct
(no child engines; 0019). Start limit 300/30 and `RestartSec=5` match
the mcremote boot-race lesson.

Deliberately **absent** on the user unit (keep it that way — **D7**):
`ProtectClock`, `ProtectKernelModules`, `ProtectKernelLogs`,
`ProtectHostname` (user-manager 218/CAPABILITIES), `ProtectHome`,
`ProtectSystem` (hide `$HOME` ACME/config).

**System example** (`deploy/systemd/mcrelay.service`) additionally
sets `ProtectSystem=strict` and the clock/module/log/hostname
protections. `User=mcrelay` and `CapabilityBoundingSet=CAP_NET_BIND_SERVICE`
remain commented — correct until an operator actually drops root.

Compared with current systemd practice (Ubuntu/Fedora hardening
guides, `systemd.exec` “relatively safe basic choice”,
`systemd-analyze security`):

| Directive | User unit now | System unit now | This MADR |
|-----------|------------------|-----------------|-----------|
| The eight + PrivateDevices + RestrictNamespaces | yes | yes | keep |
| `UMask=0077` | no | no | **D2** add |
| `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6` | no | no | **D4** after probe (Go `net` may open netlink during listen) |
| `MemoryDenyWriteExecute` | no (comment: Go cgo) | no | **D4** after probe (`CGO_ENABLED=0`) |
| `SystemCallFilter=@system-service` | no | no | **D6** later |
| `ProtectSystem=strict` | no (correct) | yes | keep on system only |
| `ProtectHome=true` | no (correct) | commented | only with `StateDirectory=` |
| `DynamicUser=` | no | no | not for user unit; optional later on system with `StateDirectory=` |
| Slim `PATH` | no | N/A (root unit) | **D1** |

`RestrictAddressFamilies` is widely recommended and usually safe, but
Go’s resolver and interface enumeration have historically used
`AF_NETLINK`. With `netgo` the pure-Go resolver talks UDP/TCP only;
a probe is still cheaper than a dark edge. Same for MDWE: modern
`CGO_ENABLED=0` Go is generally compatible, and the mcremote comment
exists because *mcremote* may load cgo-using child CLIs — mcrelay does
not.

### D. External references

* systemd.exec — “It is recommended to enforce system call allow lists
  for all long-running system services”; `RestrictAddressFamilies`
  and `MemoryDenyWriteExecute` as standard sandbox knobs.
  <https://www.freedesktop.org/software/systemd/man/systemd.exec.html>
* Ubuntu 2026 service-hardening write-up: `ProtectSystem=strict` +
  `PrivateTmp` + `NoNewPrivileges` + `SystemCallFilter=@system-service`
  as the usual baseline; enable filters in phases and test.
* CWE-1385 / CSWSH 2025: Origin check on the WebSocket handshake —
  already the coder/websocket default used in `upgrade`.
* gorilla/websocket and coder/websocket docs: default Origin policy
  rejects cross-site browsers; empty Origin (native) is the intended
  phone/host path.
* Go release builds: `CGO_ENABLED=0`, `-trimpath`, `-s -w`; linux/amd64
  PIE is the toolchain default (no extra `-buildmode=pie` required).

### E. Related records

* [0015-MADR-mcrelay-transport-security.md](0015-MADR-mcrelay-transport-security.md)
* [0016-MADR-mcrelay-audit-hardening.md](0016-MADR-mcrelay-audit-hardening.md)
* [0017-MADR-mcrelay-memory-security-action-plan.md](0017-MADR-mcrelay-memory-security-action-plan.md)
* [0019-MADR-opencode-process-management-plan.md](0019-MADR-opencode-process-management-plan.md) (`KillMode`)
* [config-mcrelay.md](config-mcrelay.md), [ops-mcrelay.md](ops-mcrelay.md)
