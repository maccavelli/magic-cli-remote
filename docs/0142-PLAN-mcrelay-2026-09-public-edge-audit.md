---
status: completed
date: 2026-09-05
associated-madr: "0142-MADR-mcrelay-2026-09-public-edge-audit.md"
owner: [Project Owner]
target-milestone: "mcrelay 2026-09 hardening pass"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Remediate the mcrelay 2026-09 public-edge audit in one hardening pass

## Executive Summary & Goal

* **Associated Decision Record**: [0142-MADR-mcrelay-2026-09-public-edge-audit.md](./0142-MADR-mcrelay-2026-09-public-edge-audit.md)
* **Goal**: Execute MADR 0142 option O1 with no remaining implementer
  choices. Every finding is in a phase, skipped with a reason, or deferred.
  Every new test has a name, a pre-fix failure, and a post-fix pass.
* **Success Criteria**:
  * [x] Phases 1–8 committed; `e2e_test.go` byte-identical to pre-flight
        (`a2d6df96ba5d509af0476e9d16e893966ff4b891`)
  * [x] Phase 9: exactly two tests named below, race suite green, floor
        still ≥ 80.0 (relay 80.7445%)
  * [ ] Named tests in the per-phase tables fail on the pre-phase tree
        and pass after
  * [ ] `go test -race -count=1 ./internal/relay/... ./internal/relayhost/...`
        green after every phase
  * [ ] `scripts/coverage-delta.sh floor --minimum 80.0` passes for
        `./internal/relay` and `./internal/relayhost`
  * [ ] `govulncheck ./...` reports 0 called vulnerabilities
  * [ ] `go fix -diff ./internal/relay/` is empty after Phase 7

---

## Finding disposition (complete)

| ID | P | Disposition | Phase |
|----|---|-------------|-------|
| F17 | P1 | IdleTimeout 120 s + HTTP/1-only Protocols | 2 |
| F18 | P2 | comma-ok receive in `phoneGone` | 1 |
| F19 | P2 | local blocking `limitedListener`, before TLS | 2 |
| F20 | P2 | `applyMemoryLimit` copy, 512 MiB | 4 |
| F21 | P2 | `MinVersion = TLS 1.3` on the ListenAndServe clone | 6 |
| F22 | P2 | owner-only config, TLS PEMs, ACME dir | 5 |
| F23 | P3 | coverage floor restored | 8 |
| F24 | P3 | `go fix ./internal/relay/` | 7 |
| F25 | P3 | `errors.Is` + `slog.Any` | 7 |
| F26 | P3 | copy mcremote signal restorer; `cmd.Context()` | 7 |
| F27 | P3 | **SKIP** — setup-service flag fork; `runSetupService` already shared | — |
| F28 | P3 | `http.Server.Protocols` HTTP/1 only, keep `stripHTTP2` | 2 |
| F29 | P3 | named `relayKeepAlive` in `server.go` only; **do not** touch `relayhost` | 7 |
| F30 | P3 | `clear(*bufPtr)` before `spliceBufPool.Put` only | 7 |
| F31 | P2 | `Secret = ""` / `Payload = nil` after hash | 3 |
| F32 | P3 | `subtle.WithDataIndependentTiming` around `checkSecret` | 7 |
| F33 | P3 | `strings.Cut`, `slices.DeleteFunc`; XFF stays a reverse loop (rate path already goes through `rateIPKey`) | 7 |
| F34 | P3 | **SKIP** — synctest rewrite of existing sleeps | — |
| F35 | P2 | reject default-route / too-wide `trusted_proxies` | 5 |
| F36 | P2 | `rateIPKey` on every `clientIP` return | 2 |
| F37 | P2 | comment + ops paragraph; no certmagic fork (no knob in v0.25.4) | 6 |
| F38 | P3 | **DEFER** — wire-visible `unknown_session` vs `unauthorized` | — |
| F39 | P3 | `parseAllowParts` error has no raw input | 3 |
| F20 apply path | P2 | `TestApplyMemoryLimitDefault` (serve-path setter) | 9 |
| F21 files-load error | P2 | `TestListenAndServeTLSFilesMissingKey` | 9 |

Kept, not in a phase: `make debug` `GOEXPERIMENT=goroutineleakprofile` (0068).
Not added: `runtime/secret`, `encoding/json/v2`, 0-RTT, Prometheus (0017 E5).

---

## Prerequisites & Dependencies

* **Toolchain**: Go 1.26.6 (`go version` must print `go1.26.6`).
* **Module**: no new direct requires. `limitedListener` is local. Do not
  `go get golang.org/x/net`. Do not import `internal/cli` from
  `internal/relay`.
* **Pre-Flight** (once, before Phase 1):

  ```sh
  go version                                          # go1.26.6
  go test -race -count=1 ./internal/relay/... ./internal/relayhost/...
  git hash-object internal/relay/e2e_test.go          # record; compare every phase
  ```

---

## Architecture & Technical Design Summary

```
TCP Accept
  └─ limitedListener (F19, cap = Limits.MaxConns, blocks)
       └─ tls.NewListener  MinVersion=1.3, stripHTTP2 (F21)
            └─ http.Server
                 IdleTimeout=120s  Protocols=HTTP/1 only  (F17, F28)
                 GET /healthz
                 GET /v1/host|phone|tunnel
                      clientIP → rateIPKey → allowRateRetry
                      checkSecret (WithDataIndependentTiming)
                      splice CopyBuffer; clear pool buf on Put
```

0115 lock discipline, error sentinels, and the 64 KiB pre-auth read-limit
ladder do not change.

### Locked constants (do not pick others)

| Name | Value | Where |
|------|-------|-------|
| `httpIdleTimeout` | `120 * time.Second` | `server.go`, `http.Server.IdleTimeout` |
| `WriteTimeout` / `ReadTimeout` | `0` (unset) | `server.go` |
| `MaxConns` default | `1024` | `DefaultLimits` |
| `MaxLimitConns` ceiling | `8192` | `config.go` with the other D9 ceilings |
| YAML / env | `limits.max_conns` / `MCRELAY_LIMITS_MAX_CONNS` | no cobra flag (other limits have none) |
| `defaultRelayMemoryLimit` | `512 << 20` (512 MiB) | `memlimit.go` |
| TLS floor | `tls.VersionTLS13` | `ListenAndServe` clone only |
| IPv4 rate key | `ip.To4().String()` | `rateIPKey` |
| IPv6 rate key | first 64 bits, string form | `rateIPKey` |
| Trusted-proxy min mask | IPv4 `/8`, IPv6 `/32` | `ParseTrustedProxies` |
| `parseAllowParts` bad shape | `fmt.Errorf("allow: want host_id:secret")` | no `%q` |
| Keepalive | `Idle 25s, Interval 5s, Count 4` | named `relayKeepAlive` |

### Cross-cutting contracts

* **Wire freeze.** Envelope JSON, message type strings, error codes, close
  reasons: unchanged. F38 is deferred so those strings stay.
* **e2e oracle.** `internal/relay/e2e_test.go` is not edited in any phase.
  `git hash-object` must match the pre-flight value.
* **One commit per phase.** `make pre-add-check FILES="…staged go…"` then
  `git commit --no-edit`. No `-m`. No push.
* **Stability after every phase:**

  ```sh
  go vet ./internal/relay/... ./internal/relayhost/...
  go test -race -count=1 ./internal/relay/... ./internal/relayhost/...
  git hash-object internal/relay/e2e_test.go   # == pre-flight
  ```

* **Test-first.** For every test named below: add it, run it, watch it
  **FAIL**, then implement, then watch it **PASS**. Commit message is
  hook-generated; the body of the work is the failing-then-passing test.

---

## Phased Implementation Plan

### Phase 1 — F18 join-slot reverse order

**Files (only):** `internal/relay/hub.go`, `internal/relay/join_race_test.go`.

**1.1** In `join_race_test.go`, immediately after `TestAbandonAfterPhoneGone`
(line 135), add `TestPhoneGoneAfterAbandon`:

```go
func TestPhoneGoneAfterAbandon(t *testing.T) {
	h := raceHub(t)
	if _, err := h.beginJoin("h1", nil); err != nil {
		t.Fatal(err)
	}
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	h.abandonTunnel(p)
	if orphan := h.phoneGone(p); orphan != nil {
		t.Fatalf("phoneGone after abandon returned %v, want nil", orphan)
	}
	if got := h.phoneCount("h1"); got != 1 {
		t.Fatalf("phones=%d, want 1 (exactly-once release)", got)
	}
	if !doneClosed(p) {
		t.Fatal("done must be closed after abandon")
	}
	assertNoDivergence(t, h)
}
```

Run `go test -race -count=1 -run TestPhoneGoneAfterAbandon ./internal/relay/`.
**Required result: FAIL** with `phones=0, want 1`.

**1.2** Replace the claimed-join `select` in `phoneGone` (`hub.go` ~370–377)
with exactly:

```go
	p.phoneGone = true
	select {
	case t, ok := <-p.ready:
		if !ok {
			return nil
		}
		h.releasePhoneLocked(p.hostID)
		p.closeDone()
		return t
	default:
		return nil
	}
```

Do not change `abandonTunnel`. Do not close `ready` in `phoneGone` on this
path.

**1.3** Re-run:

```sh
go test -race -count=1 -run 'TestPhoneGoneAfterAbandon|TestAbandonAfterPhoneGone|TestPhoneGoneAfterClaimBeforePublish|TestPhoneGoneAfterPublish|TestPhoneGonePendingIsCancel' ./internal/relay/
```

All pass. `assertNoDivergence` is inside each.

**Commit** those two files.

---

### Phase 2 — F17 IdleTimeout, F19 max_conns, F28 Protocols, F36 rateIPKey

**Files (only):**

* `internal/relay/server.go`
* `internal/relay/config.go`
* `internal/relay/fileconfig.go`
* `internal/relay/clientip.go`
* `internal/relay/conns.go` *(create)*
* `internal/relay/conns_test.go` *(create)*
* `internal/relay/clientip_test.go`
* `internal/relay/fileconfig_test.go` *(MaxConns validate/clamp rows)*
* `docs/config-mcrelay.md`

**2.1 `http.Server` in `New` (`server.go` ~111–122).** Set, and do not set
anything else:

```go
	p := new(http.Protocols)
	p.SetHTTP1(true)
	s.http = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       httpIdleTimeout(),
		MaxHeaderBytes:    maxHeaderBytes,
		Protocols:         p,
		ErrorLog:          stdlog.New(&tlsHandshakeLogFilter{lg: s.log}, "", 0),
	}
```

`WriteTimeout` and `ReadTimeout` stay unset.

Copy the `firstEnvelopeTimeout` pattern (`server.go` ~160–172) for idle:

```go
var httpIdleTimeoutNanos atomic.Int64

func init() { httpIdleTimeoutNanos.Store(int64(120 * time.Second)) }

func httpIdleTimeout() time.Duration {
	return time.Duration(httpIdleTimeoutNanos.Load())
}

func setHTTPIdleTimeout(d time.Duration) time.Duration {
	return time.Duration(httpIdleTimeoutNanos.Swap(int64(d)))
}
```

**2.2 Limits plumbing.** In `config.go`:

* `MaxLimitConns = 8192` next to the other D9 ceilings.
* `Limits.MaxConns int` field after `MaxConcurrentJoin`.
* `DefaultLimits`: `MaxConns: 1024`.
* `ResolvedLimits`: `if l.MaxConns <= 0 { l.MaxConns = d.MaxConns }`.
* `ClampLimits`: `l.MaxConns = clampInt(l.MaxConns, 1, MaxLimitConns)`.

In `fileconfig.go` `LimitsConfig`: `MaxConns int \`mapstructure:"max_conns"\``.
`DefaultsFile` copies `d.MaxConns`. `setFileDefaults`:
`v.SetDefault("limits.max_conns", d.Limits.MaxConns)`.
`BindEnv("limits.max_conns", "MCRELAY_LIMITS_MAX_CONNS")`.
`validateLimitsConfig`: `check("max_conns", l.MaxConns, MaxLimitConns)`.
`ToServerConfig`: `if c.Limits.MaxConns > 0 { lim.MaxConns = c.Limits.MaxConns }`.
No cobra flag.

**2.3 `conns.go` — blocking limited listener.** Do not import
`golang.org/x/net`. Shape:

```go
type limitedListener struct {
	net.Listener
	sem chan struct{}
}

func limitListener(ln net.Listener, n int) net.Listener {
	if n <= 0 {
		return ln
	}
	return &limitedListener{Listener: ln, sem: make(chan struct{}, n)}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitedConn{Conn: c, release: l.sem}, nil
}

type limitedConn struct {
	net.Conn
	release chan struct{}
	once    sync.Once
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.release })
	return err
}
```

In `ListenAndServe`, **immediately after** `lc.Listen` succeeds and
**before** the TLS `switch`:

```go
	ln = limitListener(ln, s.cfg.Limits.MaxConns)
```

Do not wrap again inside `Serve`. Tests that call `Serve` directly do not
exercise the cap; the MaxConns test uses `ListenAndServe`.

**2.4 `rateIPKey` in `clientip.go`.** Trusted-proxy matching stays on the
unmasked IP. Canonicalize only the value returned for rate keys:

```go
func rateIPKey(host string) string {
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	v6 := ip.To16()
	if v6 == nil {
		return host
	}
	out := make(net.IP, net.IPv6len)
	copy(out, v6)
	for i := 8; i < 16; i++ {
		out[i] = 0
	}
	return out.String()
}
```

At every `return` of `clientIP` (four today: remote, XFF hop, X-Real-IP,
fallback), return `rateIPKey(...)`. Existing IPv4 tests keep the same
dotted-quad strings.

**2.5 Tests — add, watch FAIL, then implement.**

| Test | File | Pre-fix | Post-fix |
|------|------|---------|----------|
| `TestHealthzIdleTimeout` | `conns_test.go` | keep-alive still readable after 150 ms if IdleTimeout is 0 | with `setHTTPIdleTimeout(50*time.Millisecond)` restored via `t.Cleanup`, a second request on the same conn after 150 ms fails |
| `TestMaxConnsBlocks` | `conns_test.go` | second Dial succeeds | `Limits.MaxConns=1`, hold `/healthz` open, second `net.DialTimeout(..., 200*time.Millisecond)` fails |
| `TestRateIPKeyIPv4Mapped` | `clientip_test.go` | `::ffff:203.0.113.50` ≠ `203.0.113.50` | both `clientIP` results equal `203.0.113.50` |
| `TestRateIPKeyIPv6SameSlash64` | `clientip_test.go` | two addrs in one /64 are different keys | both keys equal |
| `TestRateIPKeyIPv6DifferentSlash64` | `clientip_test.go` | — | keys differ |
| `TestValidateMaxConnsCeiling` | `fileconfig_test.go` | 8193 accepted | `Validate` error `limits.max_conns` |

`TestHealthzIdleTimeout` / `TestMaxConnsBlocks` start the server through
`ListenAndServe` on `127.0.0.1:0` with `AllowPlaintext: true` (loopback).
Do not use `httptest.NewServer` — that bypasses `s.http` timeouts and
`limitListener`.

**2.6 Docs.** `docs/config-mcrelay.md`:

* knob table: `limits.max_conns` default `1024`, env
  `MCRELAY_LIMITS_MAX_CONNS`, yaml/env only.
* ceiling table: `limits.max_conns` → 8192.
* env table: one row for `MCRELAY_LIMITS_MAX_CONNS`.
* Trusted-proxies section: add one sentence that rate keys are IPv4
  `/32` and IPv6 `/64`. Do **not** yet add the F35 mask-floor sentence
  (Phase 5).

**Commit** the files listed at the top of this phase.

---

### Phase 3 — F31 secret references, F39 error text

**Files (only):** `internal/relay/cli.go`, `internal/relay/server.go`,
`internal/relay/config.go`, `internal/relay/config_test.go` *(or
`fileconfig_test.go` if `parseAllowParts` tests already live there)*,
`internal/relay/fileconfig.go` only if `ToServerConfig` is the cleaner
place to blank secrets (it is: every caller hashes through it).

**3.1** At the end of `ToServerConfig`, after building `creds`, blank
in place. Do not replace the slice:

```go
	for i := range c.Hosts {
		c.Hosts[i].Secret = ""
	}
```

`ToServerConfig` currently has a value receiver. **Change it to
`(c *FileConfig)`** so the blanking is visible to `serve`. Update every
call site that does not already take a pointer (`cli.go` already has
`fc` as a value — take its address: `srvCfg := fc.ToServerConfig()`
becomes `srvCfg := (&fc).ToServerConfig()` or change `fc` usage so
`Load` result is copied into a var and we call `fc.ToServerConfig()`
with pointer receiver on `*FileConfig`).

Locked form in `newServeCmd`:

```go
			srvCfg := fc.ToServerConfig() // pointer receiver; blanks fc.Hosts[].Secret
```

with

```go
func (c *FileConfig) ToServerConfig() Config {
	// ... existing creds loop using c.Hosts[i].Secret ...
	for i := range c.Hosts {
		c.Hosts[i].Secret = ""
	}
	// ... rest unchanged ...
}
```

Grep `ToServerConfig` and convert every caller to a pointer. Tests that
assert `Secret` after `ToServerConfig` must assert it is `""`.

**3.2** `handleHost`: immediately after `s.hub.checkSecret(reg.HostID, reg.Secret)`
(both branches), before any `writeErr`:

```go
	match := s.hub.checkSecret(reg.HostID, reg.Secret)
	reg.Secret = ""
	env.Payload = nil
	if !match {
```

**3.3** `handleTunnel`: immediately after `claimTunnel(...)` returns,
before using `err`:

```go
	pending, err := s.hub.claimTunnel(tun.SessionID, tun.HostID, tun.Token, tun.Secret)
	tun.Secret = ""
	tun.Token = ""
	env.Payload = nil
```

**3.4** `parseAllowParts`: the `i <= 0 || i == len(s)-1` branch becomes
`return "", "", fmt.Errorf("allow: want host_id:secret")` — no `%q`, no
`s` in the error. Other errors (`host_id too long`, `secret too short`)
already quote only `id` after charset validation; leave them.

**3.5 Tests**

| Test | Pre-fix | Post-fix |
|------|---------|----------|
| `TestToServerConfigBlanksSecrets` | `Hosts[0].Secret` still the input | `""`; `Allow[0].SecretHash` still matches `HashSecret(original)` |
| `TestParseAllowPartsErrorOmitsSecret` | `err.Error()` contains `sixteen-chars-min-secret` for input `not-a-pair-sixteen-chars-min-secret` | does not contain `sixteen-chars-min-secret` |

**Commit.**

---

### Phase 4 — F20 GOMEMLIMIT

**Files (only):** `internal/relay/memlimit.go` *(create)*,
`internal/relay/memlimit_test.go` *(create)*, `internal/relay/cli.go`,
`docs/ops-mcrelay.md`, `docs/config-mcrelay.md`.

**4.1** `memlimit.go` — copy the mcremote split, do not import
`internal/cli`:

```go
const defaultRelayMemoryLimit = 512 << 20

func memoryLimitPlan() (int64, string) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		return 0, "GOMEMLIMIT"
	}
	return defaultRelayMemoryLimit, "default"
}

func applyMemoryLimit() (int64, string) {
	lim, src := memoryLimitPlan()
	if src == "GOMEMLIMIT" {
		return debug.SetMemoryLimit(-1), src
	}
	debug.SetMemoryLimit(lim)
	return lim, src
}
```

**4.2** In `newServeCmd`, after `logging.Setup` and **before**
`ListenAndServe`:

```go
			memLimit, memLimitSource := applyMemoryLimit()
```

Add to the existing `mcrelay starting` log line:

```go
				slog.Int64("mem_limit_bytes", memLimit),
				slog.String("mem_limit_source", memLimitSource),
```

**4.3 Tests — never call `applyMemoryLimit` from a test** (it mutates
process-wide GC). Test `memoryLimitPlan` only:

| Test | Setup | Want |
|------|-------|------|
| `TestMemoryLimitPlanDefault` | `t.Setenv` cannot unset a missing key — if `GOMEMLIMIT` is already in the environment, `t.Setenv("GOMEMLIMIT", "")` then skip; else `limit, src := memoryLimitPlan()` → `512<<20, "default"` |
| `TestMemoryLimitPlanEnv` | `t.Setenv("GOMEMLIMIT", "256MiB")` | `src == "GOMEMLIMIT"` |

**4.4 Docs.** `docs/ops-mcrelay.md`: one paragraph — default 512 MiB;
`GOMEMLIMIT` in the unit file wins; raise it if `max_message_bytes` /
`max_phones_per_host` / `max_conns` are raised toward ceilings.
`docs/config-mcrelay.md`: one sentence under limits pointing at that
paragraph. Not a YAML key.

**Commit.**

---

### Phase 5 — F22 owner-only files, F35 proxy CIDRs

**Files (only):** `internal/relay/fileconfig.go`,
`internal/relay/clientip.go`, `internal/relay/fileconfig_test.go`,
`internal/relay/clientip_test.go`, `internal/relay/tls.go`,
`internal/relay/cli.go`, `docs/config-mcrelay.md`.

**5.1 Config file.** In `Load`, after a YAML file is successfully read
(`usedConfigFile != ""`), before `Unmarshal`:

```go
		ok, err := appdirs.FileIsOwnerOnly(usedConfigFile)
		if err != nil {
			return FileConfig{}, fmt.Errorf("config %s: %w", usedConfigFile, err)
		}
		if !ok {
			return FileConfig{}, fmt.Errorf("config %s is readable by group/other; chmod 0600", usedConfigFile)
		}
```

`mcrelay paths` goes through `Load`: a 0644 secrets file fails there too.
In-memory `FileConfig` (tests calling `Validate` without a file) is
unaffected. Existing Load tests already write `0o600`.

**5.2 TLS PEMs.** New `checkSecretFiles(fc FileConfig) error` in
`fileconfig.go`: if `fc.TLS.Normalized().Mode == TLSModeFiles`,
`FileIsOwnerOnly` on `CertFile` and `KeyFile`; same error shape. Call it
from `newServeCmd` after `Validate` / `Normalized` and before
`ApplyTLS`. `mcrelay paths` does not call it.

**5.3 ACME cache.** In `applyLetsEncrypt` HTTP-01 **and** DNS-01
branches, before `EnsureACMEHTTP` / `EnsureACME`:

```go
	if err := appdirs.EnsurePrivateDir(cache); err != nil {
		return nil, fmt.Errorf("acme cache: %w", err)
	}
```

`EnsurePrivateDir` requires an absolute path; `ACMECacheDir` is already
absolutized in `finalizePaths`. Do not edit `internal/certs`.

**5.4 F35.** In `ParseTrustedProxies`, after a successful `ParseCIDR`,
before append:

```go
		ones, bits := n.Mask.Size()
		switch bits {
		case 32:
			if ones < 8 {
				return nil, fmt.Errorf("trusted_proxies[%d]: IPv4 mask must be /8 or narrower", i)
			}
		case 128:
			if ones < 32 {
				return nil, fmt.Errorf("trusted_proxies[%d]: IPv6 mask must be /32 or narrower", i)
			}
		}
```

Bare IPs still become `/32` and `/128` and pass. `0.0.0.0/0` and `::/0`
fail the ones check. `10.0.0.0/8` passes.

**5.5 Tests**

| Test | Pre-fix | Post-fix |
|------|---------|----------|
| `TestLoadRejectsWorldReadableConfig` | Load of a 0644 YAML succeeds | error contains `chmod 0600` |
| `TestLoadAcceptsOwnerOnlyConfig` | — | 0600 still loads (existing tests already do this; add only if none call `Load` on a real file) |
| `TestParseTrustedProxiesRejectsDefaultRoute` | `0.0.0.0/0` and `::/0` parse | both error |
| `TestParseTrustedProxiesRejectsWideV4` | `10.0.0.0/7` parses | error `/8 or narrower` |
| `TestParseTrustedProxiesAllowsSlash8` | — | `10.0.0.0/8` and `127.0.0.1` still parse |

Windows: `FileIsOwnerOnly` is the 0116 D22 DACL implementation. Do not
build-tag the Load check out. If a Windows builder cannot create a
group-readable file, skip `TestLoadRejectsWorldReadableConfig` with
`t.Skip` **only** when `FileIsOwnerOnly` reports owner-only for the
0644-intended fixture (i.e. the OS ignored the mode).

**5.6 Docs.** Trusted-proxies section of `config-mcrelay.md`: reject
`0.0.0.0/0`, `::/0`, IPv4 wider than `/8`, IPv6 wider than `/32`.
Config-file paragraph: serve and `Load` require owner-only mode.

**Commit.**

---

### Phase 6 — F21 TLS 1.3 floor, F37 HTTP-01 docs

**Files (only):** `internal/relay/server.go`,
`internal/relay/listen_policy_test.go`, `internal/relay/tls_test.go`,
`internal/relay/server_lifecycle_test.go` *(only if it asserts 1.2)*,
`internal/certs/acme_http.go` *(one comment, no behaviour)*,
`docs/config-mcrelay.md`, `docs/ops-mcrelay.md`.

**Do not** change `internal/certs/acme.go` `TLSConfig()` MinVersion
(mcremote stays 1.2).

**6.1** In `ListenAndServe`, both TLS branches, **always** assign the
floor (do not keep `if MinVersion == 0`):

Managed:

```go
		cfg := s.cfg.TLSConfig.Clone()
		cfg.MinVersion = tls.VersionTLS13
		stripHTTP2(cfg)
```

Files:

```go
		cfg := &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
		}
		stripHTTP2(cfg)
```

Do not set `CurvePreferences`. Do not set 0-RTT / `Allow0RTT`.

**6.2** Tests that construct a `tls.Config{MinVersion: tls.VersionTLS12}`
as **input** to `New` (`listen_policy_test.go:168`, `tls_test.go:133`)
keep compiling; the listener overrides to 1.3. Add:

| Test | Pre-fix | Post-fix |
|------|---------|----------|
| `TestListenAndServeRejectsTLS12` | TLS 1.2-only dial succeeds | handshake error |
| `TestListenAndServeAcceptsTLS13` | — | default client dials `/healthz` 200 |

Client side of the reject test: `&tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12, InsecureSkipVerify: true}` against the files-mode loopback listener from `writeTestCert`.

**6.3 Docs.** `config-mcrelay.md` TLS section: outer hop is TLS 1.3 only;
TLS 1.2 scanners fail. First-party clients (relayhost, Flutter) speak
1.3. `ops-mcrelay.md`: same, plus rollback = revert the Phase 6 commit.

**6.4 F37.** certmagic v0.25.4 `httpSolver.serve` (`solvers.go:94–97`)
sets no `ReadHeaderTimeout`; `robustTryListen` returns `(nil, nil)` if
`:80` is busy. There is no issuer field for this. In
`internal/certs/acme_http.go` above `EnsureACMEHTTP`, add a comment
citing 0142 F37 and those two facts. In `ops-mcrelay.md`, one paragraph:
HTTP-01 needs exclusive ownership of the challenge port; if anything
else binds it, certmagic assumes that occupant will answer
`/.well-known/acme-challenge/` — use `tls.letsencrypt.challenge: dns-01`
on a shared host. Do not change the default challenge. Do not probe
`:80` at startup.

**Commit.** Independently revertible.

---

### Phase 7 — F24–F26, F29, F30, F32, F33

**Files:** whatever `go fix ./internal/relay/` rewrites, plus
`internal/relay/cli.go`, `internal/relay/server.go`,
`internal/relay/hub.go`, `internal/relay/tls.go`,
`internal/relay/config.go`, `internal/relay/protocol.go` *(no)*,
`internal/relayhost/` **untouched**.

**7.1** Run `go fix ./internal/relay/`. Expected: `strings.SplitSeq` in
`expandStringList` and `splitHostsCSV`; `for i := range n` in
`hub_reconcile_test.go`, `hub_test.go`, `rate_test.go`; `WaitGroup.Go` in
`hub_test.go`; `b.Context()` in `splice_bench_test.go`. Do not hand-edit
those; take the tool output. Afterward `go fix -diff ./internal/relay/`
is empty.

**7.2** `cli.go` serve return:

```go
			if err := srv.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
```

`server.go` ping line: `slog.Any("err", err)` instead of
`slog.String("err", err.Error())`. Same substitution for every
`slog.String("err", err.Error())` in `internal/relay/` (grep; currently
the ping path). Do not touch `relayhost`.

**7.3** Replace the serve signal block with mcremote's shape, using
`cmd.Context()`:

```go
			ctx, stop := signal.NotifyContext(cmd.Context(), shutdownSignals()...)
			defer stop()
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Error("signal restorer panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
					}
				}()
				<-ctx.Done()
				stop()
			}()
```

Leave `internal/relay/signals_*.go` in place (F27-class sharing is
skipped). Need `runtime/debug` import on `cli.go` if not present.

**7.4**

* `parseAllowParts`: `id, secret, ok := strings.Cut(s, ":")` after
  `TrimSpace`; `if !ok \|\| id == "" \|\| secret == ""` → the F39 error.
  Then `id = strings.TrimSpace(id)` (secret still untrimmed after the
  colon, matching 0115 F3).
* `stripHTTP2`: `cfg.NextProtos = slices.DeleteFunc(cfg.NextProtos, func(p string) bool { return p == "h2" \|\| p == "h2c" })`.
* `checkSecret`: wrap the hash+compare body in
  `subtle.WithDataIndependentTiming(func() { ... })` and return the
  result from a named bool. Import `crypto/subtle` is already in `hub.go`.
* `copyDir` defer Put: `clear(*bufPtr); spliceBufPool.Put(bufPtr)`.
* `server.go` ListenConfig: replace the literal with

  ```go
  var relayKeepAlive = net.KeepAliveConfig{
      Enable: true, Idle: 25 * time.Second, Interval: 5 * time.Second, Count: 4,
  }
  ```

  and `KeepAliveConfig: relayKeepAlive`. Do not change `relayhost`.

**7.5 SKIP F27, F34, F33 XFF `slices.Backward`.** The XFF loop remains
index-based; F36 already canonicalizes the returned IP. Do not convert
idle tests to synctest.

**Verification:** `go fix -diff ./internal/relay/` empty; stability rule;
e2e hash unchanged.

**Commit.**

---

### Phase 8 — F23 coverage, docs sweep, regression

**Files:** additional `*_test.go` under `internal/relay/` only if
`coverage-delta.sh floor` fails after 8.1; `docs/ops-mcrelay.md`;
`docs/config-mcrelay.md` only for leftover mismatches.

**8.1** Capture and floor:

```sh
scripts/coverage-snapshot.sh --output /tmp/0142-after \
  --go ./internal/relay --go ./internal/relayhost
scripts/coverage-delta.sh floor --after /tmp/0142-after --minimum 80.0 \
  --go ./internal/relay --go ./internal/relayhost
```

If `internal/relay` is below 80.0, add tests only for uncovered
**failure** paths the new code introduced (`limitedConn.Close` once,
`rateIPKey` nil-parse, `memoryLimitPlan` env branch, `checkSecretFiles`
missing key). Do not test trivial accessors. Re-run 8.1 until floor
exits 0. `internal/relayhost` must stay ≥ 80.0 with **zero** edits.

**8.2** Docs sweep — confirm these sentences exist; add only what is
missing after Phases 2–6:

* IdleTimeout 120 s; `WriteTimeout` unset on purpose
* `limits.max_conns` default 1024, ceiling 8192, blocking Accept
* IPv6 /64 rate keys; trusted-proxy mask floor
* GOMEMLIMIT 512 MiB default
* TLS 1.3 floor
* config 0600 enforcement
* HTTP-01 exclusive :80 / DNS-01 on shared hosts
* F18 reverse-order is exact; slot-sweep WARNs for that cause stay gone

**8.3** Final regression:

```sh
go vet ./internal/relay/... ./internal/relayhost/...
go build -o /tmp/mcrelay-0142 ./cmd/mcrelay
go test -race -count=1 ./internal/relay/... ./internal/relayhost/...
govulncheck ./...
scripts/coverage-delta.sh floor --after /tmp/0142-after --minimum 80.0 \
  --go ./internal/relay --go ./internal/relayhost
git hash-object internal/relay/e2e_test.go   # == pre-flight
go fix -diff ./internal/relay/               # empty
```

**8.4** After the owner accepts the MADR: append **Observed** to
`0142-MADR` (coverage numbers, Phase 6 smoke, skipped F27/F34). The
owner flips `status: proposed` → `accepted`. The implementer does not.

**Commit** Phase 8 (tests/docs only). Observed is a later docs commit
after accept, not this phase.

---

### Phase 9 — two more tests (F20 apply path, F21 files-load error)

**Objective.** Cover the two 0142 production functions that Phase 8 left
at 0% / untested-failure: `applyMemoryLimit` and
`ListenAndServe`'s `LoadX509KeyPair` error. No production edits.

**Files (only):** `internal/relay/memlimit_test.go`,
`internal/relay/listen_policy_test.go`.

**Do not** edit `memlimit.go`, `server.go`, `e2e_test.go`, or
`internal/relayhost`.

**9.1** Append to `memlimit_test.go` exactly (imports: add `"runtime/debug"`
if not present):

```go
func TestApplyMemoryLimitDefault(t *testing.T) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		t.Skip("GOMEMLIMIT already set in environment")
	}
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	lim, src := applyMemoryLimit()
	if src != "default" {
		t.Fatalf("src=%q, want default", src)
	}
	if lim != defaultRelayMemoryLimit {
		t.Fatalf("lim=%d, want %d", lim, defaultRelayMemoryLimit)
	}
}
```

No `t.Parallel()`. The cleanup restores the process limit so later tests
in the same `go test` process are not left under 512 MiB.

**9.2** Append to `listen_policy_test.go` exactly:

```go
func TestListenAndServeTLSFilesMissingKey(t *testing.T) {
	srv := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Allow:       []HostCredential{testCred(t)},
		TLSCertFile: "/no/such/mcrelay-cert.pem",
		TLSKeyFile:  "/no/such/mcrelay-key.pem",
		Limits:      DefaultLimits(),
	}, nil)
	err := srv.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("ListenAndServe with missing PEMs succeeded")
	}
}
```

This hits `tls.LoadX509KeyPair` after `limitListener` wrap
(`server.go:253–256`): the bound listener must be closed and the error
returned. Do not use `httptest`. `requireTLSOrLoopback` is satisfied by
the cert/key paths being non-empty.

**9.3** Run, in order:

```sh
go test -race -count=1 -run 'TestApplyMemoryLimitDefault|TestListenAndServeTLSFilesMissingKey' ./internal/relay/
go test -race -count=1 ./internal/relay/... ./internal/relayhost/...
git hash-object internal/relay/e2e_test.go   # == a2d6df96ba5d509af0476e9d16e893966ff4b891
scripts/coverage-snapshot.sh --output /tmp/0142-p9 \
  --go ./internal/relay --go ./internal/relayhost
scripts/coverage-delta.sh floor --after /tmp/0142-p9 --minimum 80.0 \
  --go ./internal/relay --go ./internal/relayhost
```

`TestApplyMemoryLimitDefault` may `Skip` if this environment has
`GOMEMLIMIT` set; that is a pass, not a fail. The TLS-missing-key test
must run.

**9.4** `make pre-add-check FILES="internal/relay/memlimit_test.go internal/relay/listen_policy_test.go"`
then `git add` those two files and `git commit --no-edit`. No `-m`. No
push. Append one Observed bullet to the MADR with the new floor numbers
in that same commit.

---

## Verification & Testing Strategy

| Level | Command / artefact | Pass |
| :--- | :--- | :--- |
| Pre-fix proof | each named test, run before its production edit | FAIL with the asserted diagnostic |
| Unit / package | `go test -race -count=1 ./internal/relay/... ./internal/relayhost/...` | green, every phase |
| e2e freeze | `git hash-object internal/relay/e2e_test.go` | equals pre-flight |
| Cover | `coverage-delta.sh floor --minimum 80.0` | exit 0 both packages |
| Idioms | `go fix -diff ./internal/relay/` | empty after Phase 7 |
| Vuln | `govulncheck ./...` | 0 called |
| Live (after deploy, not a phase) | `d4_live_test.go` pattern | register, join, splice, SIGTERM drain |

---

## Rollback & Mitigation Procedures

* **Trigger:** first-party TLS handshake failures, join-slot WARNs,
  accept stalls under normal load, GC thrash / OOM, serve refuse of a
  previously-working 0644 config.
* **Per-phase revert:** `git revert` that phase's commit. Phase 6 is the
  only operator-visible TLS change and reverts alone.
* **F20 without a revert:** set `GOMEMLIMIT=` in the systemd unit; it
  wins over the 512 MiB default.
* **F22 without a revert:** `chmod 0600` the config and PEMs.
* **F19 without a revert:** raise `limits.max_conns` (ceiling 8192) or
  `MCRELAY_LIMITS_MAX_CONNS`.
* **Full unwind:** revert Phases 9 → 1. No migrations. Unknown YAML
  `limits.max_conns` is ignored by an older binary.
* **Phase 9:** tests only; revert is `git revert` of that commit. No
  behaviour change.
* **Production:** `mcrelay update --force` to the prior release. No push
  in this plan.

---

## Task Progress Checklist

- [ ] Pre-flight: `go version` is 1.26.6; race suite green; e2e hash recorded
- [ ] **Phase 1 F18**
  - [ ] 1.1 `TestPhoneGoneAfterAbandon` fails (`phones=0`)
  - [ ] 1.2 comma-ok `phoneGone`
  - [ ] 1.3 all `TestPhoneGone*` / `TestAbandon*` pass
  - [ ] commit
- [ ] **Phase 2 F17 F19 F28 F36**
  - [ ] 2.1 IdleTimeout + Protocols
  - [ ] 2.2 MaxConns plumbing
  - [ ] 2.3 `conns.go` wrap before TLS
  - [ ] 2.4 `rateIPKey`
  - [ ] 2.5 named tests fail then pass
  - [ ] 2.6 `config-mcrelay.md`
  - [ ] commit
- [ ] **Phase 3 F31 F39**
  - [ ] 3.1 pointer `ToServerConfig` blanks secrets
  - [ ] 3.2 `handleHost`
  - [ ] 3.3 `handleTunnel`
  - [ ] 3.4 generic allow error
  - [ ] 3.5 named tests fail then pass
  - [ ] commit
- [ ] **Phase 4 F20**
  - [ ] 4.1 `memlimit.go`
  - [ ] 4.2 serve log fields
  - [ ] 4.3 `memoryLimitPlan` tests only
  - [ ] 4.4 docs
  - [ ] commit
- [ ] **Phase 5 F22 F35**
  - [ ] 5.1 Load owner-only
  - [ ] 5.2 PEM check
  - [ ] 5.3 `EnsurePrivateDir` on ACME cache
  - [ ] 5.4 proxy mask floor
  - [ ] 5.5 named tests fail then pass
  - [ ] 5.6 docs
  - [ ] commit
- [ ] **Phase 6 F21 F37**
  - [ ] 6.1 MinVersion TLS1.3 on clone
  - [ ] 6.2 reject-1.2 / accept-1.3 tests
  - [ ] 6.3 docs
  - [ ] 6.4 HTTP-01 comment + ops paragraph
  - [ ] commit
- [ ] **Phase 7 idioms**
  - [ ] 7.1 `go fix` empty diff
  - [ ] 7.2 `errors.Is` / `slog.Any`
  - [ ] 7.3 signal restorer + `cmd.Context()`
  - [ ] 7.4 Cut, DeleteFunc, WithDataIndependentTiming, pool clear, `relayKeepAlive`
  - [ ] F27 F34 F33-XFF skipped
  - [ ] commit
- [ ] **Phase 8 F23**
  - [ ] 8.1 floor ≥ 80.0 both packages
  - [ ] 8.2 docs sweep
  - [ ] 8.3 final vet/build/race/govulncheck/fix/e2e-hash
  - [ ] 8.4 Observed waits for owner accept
  - [ ] commit
- [x] **Phase 9 two more tests**
  - [x] 9.1 `TestApplyMemoryLimitDefault` in `memlimit_test.go`
  - [x] 9.2 `TestListenAndServeTLSFilesMissingKey` in `listen_policy_test.go`
  - [x] 9.3 race suite + floor ≥ 80.0 + e2e hash
  - [x] 9.4 pre-add-check and commit those two files only
