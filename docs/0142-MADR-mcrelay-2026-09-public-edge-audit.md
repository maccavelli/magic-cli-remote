---
status: accepted
date: 2026-09-05
decision-makers: maccavelli
consulted: —
informed: —
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Remediate the mcrelay 2026-09 public-edge audit in one hardening pass

## Context and Problem Statement

The owner asked for a fresh, deep evaluation of the **mcrelay** binary:
security of the public join plane, Go 1.26.6 idioms and memory management,
duplication against `mcremote`, and residual bugs. mcrelay last received a
dedicated pass in [0115](./0115-MADR-mcrelay-go126-audit-and-hardening.md)
(2026-08-25, Go 1.26.5, findings F1–F16 shipped). The toolchain is now
**Go 1.26.6**; Green Tea GC is the default collector; `crypto/tls` enables
post-quantum hybrid KEMs on TLS 1.3 by default. The production listener is
still a public internet edge.

The audited surface is the binary and what it links:
`cmd/mcrelay/main.go`, `internal/relay/` (whole package), and the ACME path it
calls in `internal/certs`. `internal/relayhost` is the host-side client inside
`mcremote`, not this binary; it is cited only where duplication or the join
handshake would otherwise be missed.

The question this record decides: **which of this audit's findings are worth
fixing, in what shape, and what is explicitly accepted as-is.**

### What the audit confirms is in good shape (facts, with evidence)

Prior MADRs are still load-bearing. None of the following needs re-work:

* **Trust model (0015 D1/D2).** The relay is a join-plane router and opaque
  byte splice. It does not parse protocol-v1. Inner TLS to `mcremote` is
  preserved; splice copies `Reader`/`Writer` with a pooled 32 KiB buffer
  (`internal/relay/server.go:80–89`, `:909–942`).
* **Auth and enumeration resistance (0017 D7/D12/D13, 0115 F3/F8).** Allowlist
  secrets are SHA-256 (`config.go:206–208`); `checkSecret` always hashes and
  compares in constant time, including unknown ids (`hub.go:91–103`); unknown
  host and wrong secret return the same `unauthorized`; join of an unknown
  `host_id` is `host_offline` (`hub.go:199–202`); tunnel claims prefer a
  single-use 32-byte token (`hub.go:250–265`); legacy secret-on-tunnel is
  default-off.
* **0115 F1–F7 did not regress in the direction that was tested.**
  `phoneGone` / `publishTunnel` / `abandonTunnel` exist; pre-auth
  `SetReadLimit(ControlReadLimitBytes)` is on all three planes before the
  first envelope (`server.go:519–520`, `:635–636`, `:767–768`); `--allow`
  parsing trims once (`parseAllowParts`); rate-map eviction is oldest-window
  (`evictOldestRateLocked`); envelope I/O is canonical in `protocol.go`.
* **Edge hardening (0091, 0016).** TLS floor 1.2 with h2 ALPN stripped;
  16 KiB header cap; plaintext refused off-loopback; WS origin empty
  (native + same-origin, not `*`); first-envelope timeout 10 s; kernel
  keepalive 25/5/4; compression **disabled** by coder/websocket default
  (`AcceptOptions.CompressionMode` zero value is `CompressionDisabled`).
* **Release pprof is not in the binary.** `debugserve.Start` is a no-op
  without the `debugpprof` tag (`internal/debugserve/debugserve_off.go`).
* **govulncheck (this session).** `govulncheck ./...` reports 0 called
  vulnerabilities. Three unused-module findings sit in `golang.org/x/crypto`
  SSH/OpenPGP, which this tree does not call.
* **Classic memory unsafety.** No `unsafe`, no cgo in this binary's packages,
  no integer-wrap RCE on the splice path. Go 1.26 heap-base randomization is
  on. Green Tea GC is the 1.26 default — no `GOEXPERIMENT` pin required.

### Findings (facts, each verified against source)

IDs continue the **F** series from 0115 so logs and PRs cite one namespace.

Severity: **P0** remotely reachable RCE / auth bypass / inner-TLS break;
**P1** unauthenticated resource exhaustion or a clear policy hole on the
public edge; **P2** hardening or correctness under failure; **P3** idiom,
duplication, polish.

**No P0 was found.**

#### Bugs and public-edge gaps

* **F17 — P1 — `http.Server` has no `IdleTimeout`; `/healthz` and 404s are
  unrate-limited.** `New` sets `ReadHeaderTimeout: 10s` and `MaxHeaderBytes:
  16 KiB` (`server.go:111–122`) and nothing else. Go's zero `IdleTimeout`
  with zero `ReadTimeout` means keep-alive never dies. `allowRateRetry` runs
  only inside `upgrade` (`server.go:489–495`), so a client that never
  upgrades — `GET /healthz`, or any 404 — is unbounded per IP. `mcremote`
  already sets `IdleTimeout: 120 * time.Second` for this reason
  (`internal/daemon/daemon.go:461–465`) and documents why `WriteTimeout`
  stays 0 (it would kill hijacked WebSockets). Hijacked splices are
  unaffected by `IdleTimeout`; HTTP keep-alives are not.

* **F18 — P2 — `phoneGone` after `abandonTunnel` double-releases the phone
  slot.** 0115 F1 closed the timeout/claim race in one order.
  `TestAbandonAfterPhoneGone` (`join_race_test.go:135–158`) only covers
  **phoneGone then abandonTunnel**. The reverse is untested and wrong.
  `abandonTunnel` always `close(p.ready)` (`hub.go:304–306`). `phoneGone` on
  a claimed join then does `select { case t := <-p.ready: ... release ... }`
  (`hub.go:370–377`). A receive from a **closed** channel is immediately
  ready in a `select` (it does not take `default`) and yields a nil conn, so
  `releasePhoneLocked` runs a second time. `releasePhoneLocked` will not go
  negative (`hub.go:387–390`), but with a second live join as canary the
  durable counter drops one extra slot. `MaxPhonesPerHost` can then be
  exceeded until the two-sweep healer (~60 s). Window: handlePhone has
  already chosen `timer.C` / `ctx.Done` and has not yet called `phoneGone`
  when handleTunnel's `tunnel_ok` write fails. Not P1: it requires the host
  tunnel write to fail in the same instant as the phone timeout; it is still
  a pairing bug of the class 0115 existed to kill.

* **F19 — P2 — no cap on concurrent accepted connections.** Accept rate is
  120/min/**IP** after the HTTP upgrade (`DefaultLimits`). There is no
  process-wide `LimitListener` / `max_conns`. TLS handshakes are therefore
  unbounded: a botnet of distinct addresses, or a single address that never
  upgrades, can fill the fd table. `mcremote`'s WS server has `maxClients`
  (`internal/ws/server.go:53`, `:592`); mcrelay has no analogue. F17's
  keep-alive is the cheap path; F19 is the remaining one after IdleTimeout.
  This is not a reopen of 0091 R47 (no global *splice* cap, still accepted):
  it is a cap on **accepted TCP/TLS connections**, including `/healthz`.

* **F20 — P2 — mcrelay never sets `GOMEMLIMIT`.** [0138](./0138-MADR-overhaul-provider-surfaces-and-turn-path.md)
  added `applyMemoryLimit` (`internal/cli/serve.go:154–165`, default 1 GiB)
  so a retention bug in **mcremote** degrades into GC pressure instead of an
  OOM. `cmd/mcrelay` / `internal/relay/cli.go` do not call it. The public
  splice can hold `MaxMessageBytes` (default 1 MiB, ceiling 16 MiB) in both
  directions per live tunnel; without a soft heap ceiling a leak or a
  hostile host with a raised limit is an OOM. Go 1.26's Green Tea GC reduces
  overhead; it is not a substitute for a limit.

* **F21 — P2 — TLS floor is still 1.2 (0115 O4, still deferred).**
  `ListenAndServe` writes `MinVersion: tls.VersionTLS12` for files mode and
  fills 0 with 1.2 for managed ACME (`server.go:227–241`);
  `certs.ACMEBundle.TLSConfig` does the same (`internal/certs/acme.go:176`).
  First-party clients (Go `relayhost`, Flutter) speak 1.3. Go 1.26 enables
  `SecP256r1MLKEM768` / `SecP384r1MLKEM1024` **only on TLS 1.3**. Staying on
  1.2 also keeps the remaining TLS 1.2 CBC suites in the default set. This
  is no longer "defer until the twelve-finding pass is bisectable": it can
  be its own phase.

* **F22 — P2 — serve does not check that secret-bearing files are
  owner-only.** Host registration secrets live in YAML (or
  `MCRELAY_HOSTS`). `setup-service` writes 0600 and the docs say so
  (`cli.go:372–373`); `EnsureDataDir` is 0700. `Load` never calls
  `appdirs.FileIsOwnerOnly` on `cfg.ConfigFile`. Files-mode TLS keys go
  through `tls.LoadX509KeyPair` with no mode check (`server.go:234–237`).
  ACME storage is `os.MkdirAll(..., 0o700)` (`certs/acme_http.go:60–62`),
  which does **not** chmod an already-0755 `acme/` tree. A world-readable
  `config.yaml` or a pre-created group-readable cache starts the public
  edge without a word.

* **F23 — P3 (process) — `internal/relay` coverage slipped under the 80%
  floor 0115 set.** This session: `go test -count=1 -cover` → **relay 79.8%**,
  **relayhost 81.2%**. 0115 Observed was 80.7% / 81.2%. The floor still has
  no automated gate in CI for these two packages (0113's enumerated list
  never included them; 0115 claimed ownership and the number drifted).

* **F35 — P2 — `trusted_proxies` accepts default-route CIDRs.**
  `ParseTrustedProxies` takes any parseable CIDR (`clientip.go:10–37`);
  `Validate` only checks parse success. If `RemoteAddr` is inside the
  trusted set, the rightmost untrusted `X-Forwarded-For` hop becomes the
  rate-limit id (`clientip.go:67–95`). `trusted_proxies: ["0.0.0.0/0"]`
  (or `::/0`) makes every client a "proxy": anyone can rotate XFF and
  reset accept/join/register windows. Empty default is still fail-closed;
  the hole is an operator foot-gun with full R16 bypass.

* **F36 — P2 — rate keys are raw remote IPs (no IPv6 /64 aggregation,
  no IPv4-mapped canonicalization).** `clientIP` returns
  `SplitHostPort(RemoteAddr)` as-is (`clientip.go:52–58`). Default listen
  is `0.0.0.0` (IPv4-only), so production on that bind is unaffected.
  An operator who binds `::` or dual-stack sees one subscriber as a /64
  of ephemeral addresses; `AcceptPerMinute=120` becomes per-address, not
  per-subscriber. The usual IPv6 rate-limit bypass of H3.

* **F37 — P2 — ACME HTTP-01 solver: no header timeout; bind fail-open.**
  `EnsureACMEHTTP` hands the challenge to certmagic v0.25.4. That
  solver's `http.Server` sets **no** `ReadHeaderTimeout` (certmagic's
  high-level `HTTPS()` helper uses 5 s; this path does not), and
  `robustTryListen` returns **success with a nil listener** if `:80` is
  already occupied, assuming the occupant will serve
  `/.well-known/acme-challenge/`. During `ManageSync` / renewal, port 80
  is a slowloris target; a local occupant of :80 can intercept issuance
  and, if it answers the challenge, obtain a publicly trusted cert for
  the relay name. Handler is challenge-only (other paths 404). DNS-01
  does not have this listener. 0016 R13 / 0091 R45 accepted "we need
  :80"; they did not cover fail-open or missing timeouts.

* **F38 — P3 — `/v1/tunnel` distinguishes missing vs wrong session.**
  `claimTunnel` returns `unknown_session` if the id is not pending,
  `unauthorized` if the host/token is wrong (`hub.go:243–265`). That is
  an existence oracle. Session ids are `uuid.NewString()` (122 bits) —
  not a practical brute-force. Register/join enumeration (R10) was
  closed; tunnel was not given the same uniform error.

* **F39 — P3 — malformed `--allow` / `MCRELAY_HOSTS` errors quote the
  secret.** `parseAllowParts` on a missing colon does
  `fmt.Errorf("allow: want host_id:secret, got %q", s)` (`config.go:180–184`).
  A systemd `Environment=` typo prints the secret on stderr/journal.
  Startup-only, operator-local. 0115 F3 fixed whitespace hashing, not
  this error text.

#### Secret hygiene (not a wipe; Go strings are immutable)

* **F31 — P2 — plaintext registration secrets remain reachable for the
  whole control / splice lifetime.** `ToServerConfig` hashes into
  `Config.Allow` (`fileconfig.go:702–709`) but `serve`'s `fc FileConfig`
  still holds `Hosts[].Secret` until `ListenAndServe` returns.
  `handleHost` keeps `reg RegisterPayload` on the stack across
  `<-hostCtx.Done()` (`server.go:532–598`) — the secret rides the host
  control connection, which is days. `handleTunnel` keeps `tun.Secret`
  until `<-pending.done` (`server.go:780–828`) — the splice, up to
  `SpliceMax` (default 12 h). Go cannot wipe a `string`; dropping the
  reference (`reg.Secret = ""`, clear `fc.Hosts` after hashing, drop
  `env.Payload`) makes the bytes GC-eligible. `runtime/secret` is still
  `GOEXPERIMENT=runtimesecret` on 1.26.6 — not adopted here.

#### Go 1.26.6 idioms and canonicalization (P3)

0115 F8–F15 did **not** regress. `go fix -diff ./internal/relay/` (this
session, Go 1.26.6 modernizers) still wants:

* **F24** — `strings.SplitSeq` in `expandStringList` / `splitHostsCSV`;
  `for i := range n` in tests; `WaitGroup.Go` in `hub_test.go`;
  `b.Context()` in `splice_bench_test.go`.
* **F25** — `err != context.Canceled` in `cli.go:281` should be
  `errors.Is`; ping/host logs use `slog.String("err", err.Error())`
  instead of `slog.Any("err", err)`.
* **F26** — `shutdownSignals` + tests are a byte-for-byte fork of
  `internal/cli/signals_*.go`. mcremote re-arms default handling after the
  first signal so a second Ctrl-C kills a stuck drain
  (`internal/cli/serve.go:85–97`); mcrelay does not (`cli.go:255–256`).
  Drain timeout is 5 s, so this is stuck-drain UX, not a hang forever.
* **F27** — `setupServiceFlags` / `bindSetupServiceFlags` remain a product
  fork of `internal/cli/setup_service.go`. `runSetupService` already calls
  the shared `internal/cli/service` package.
* **F28** — `certs.ACMEBundle.TLSConfig` prepends `"h2"` (`acme.go:177`)
  because mcremote wants HTTP/2; mcrelay then `stripHTTP2` in four places.
  `http.Server.Protocols` (Go 1.24) is the stdlib "HTTP/1 only" knob and
  is unset.
* **F29** — keepalive `25s/5s/4` is copied by hand in `server.go:212–218`
  and `relayhost/client.go`; `config.KeepaliveConfig.NetConfig()` already
  exists for mcremote.
* **F30** — splice/bridge `sync.Pool` does not `clear` the buffer on `Put`.
  Next `CopyBuffer` overwrites as it goes (no cross-session send), but a
  crash dump retains the previous frame. Inner traffic is TLS ciphertext;
  still cheap defense in depth.
* **F32** — `checkSecret` is already constant-time on the digest;
  wrapping the hash+compare in `crypto/subtle.WithDataIndependentTiming`
  (Go 1.26: inherited by spawned goroutines, no thread lock) is the
  current stdlib idiom for secret work.
* **F33** — `parseAllowParts` is `IndexByte` not `strings.Cut`; XFF walk
  is a C-style reverse loop not `slices.Backward`; `stripHTTP2` is a
  manual filter not `slices.DeleteFunc`.
* **F34** — idle / first-envelope / rate-TTL tests sleep on the real
  clock. Go 1.24+ `testing/synctest` would make them deterministic.
  `t.Context()` is unused.

### Explicitly not findings

* TLS 1.2 *as a defect in 0115* — recorded there as O4 deferred; re-opened
  here as F21 because the toolchain and the requirement have moved.
* `encoding/json/v2` — still `GOEXPERIMENT` on 1.26.6; it is a stdlib
  package in 1.27. Join-plane envelopes are tiny; splice is not JSON.
* `runtime/secret` — experimental, amd64/arm64 Linux only.
* 0-RTT / early data — wrong for a non-idempotent WebSocket upgrade.
* Prometheus metrics — 0017 E5, still deferred.
* `/healthz` body (`{"ok":true}`) — 0016 R11, by design.
* Compression on the join plane — already disabled.
* `InsecureSkipVerify` in `relayhost` — explicit host-side dev flag, not
  this binary.
* SHA-256 rather than a password KDF for registration secrets — operator
  secrets are high-entropy shared secrets, not user passwords; plaintext
  already sits in the config file. Raising the 16-character floor is a
  product decision, not this pass.
* Green Tea GC / container-aware `GOMAXPROCS` — already the 1.26 default;
  do not pin `GOEXPERIMENT`.

## Decision Drivers

* The relay is a **public internet edge**. Unauthenticated connection hold
  (F17/F19) and self-reclamation of join slots (F18) outrank idiom work.
* Prior relay MADRs set a precedent: findings become numbered decisions
  with regression tests, not drive-by fixes.
* Go 1.26.6 gives TLS 1.3 post-quantum KEMs for free **once the floor
  moves**; leaving F21 deferred another cycle wastes that.
* No protocol or wire-format change is on the table — every fix must be
  invisible to existing phones and hosts, except F21 (TLS 1.2 clients,
  which first-party code is not).
* 0115's lesson stands: untested failure-path orders are where the next
  F1 lives. F18 is exactly that.

## Considered Options

* **O1 — one hardening pass: fix F17–F22, F31, F35–F37 and F39; raise
  the TLS floor to 1.3 (F21); restore the 80% coverage floor (F23); take
  the mechanical idiom/canonicalization items F24–F30 / F32–F33. Defer
  F38 (tunnel existence oracle; 122-bit UUIDs).**
* **O2 — P1/P2 only (F17–F22, F31, F35–F37); defer TLS 1.3, idioms, coverage**
* **O3 — record findings, fix nothing now**
* **O4 — O1 without the TLS 1.3 floor (repeat 0115's deferral of O4)**

## Decision Outcome

Chosen option: **"O1 — one hardening pass including the TLS 1.3 floor"**,
because F17/F19 are live unauthenticated behaviours on a public listener,
F18 is the untested reverse of a race 0115 already decided must be exact,
F35 is a one-line Validate reject that otherwise bypasses every rate
bucket, and F21 is now a one-phase, independently revertible change
rather than a rider on a twelve-finding diff. Idiom work (F24–F33)
touches the same files the bug fixes will already rewrite; splitting it
multiplies review without reducing risk. Coverage is in the same pass
because 0115's floor already slipped once with no owner in CI. F38 is
deferred: closing the tunnel existence oracle is a wire-visible error
string change and the id space is 122 bits.

O4 (defer TLS 1.3 again) is **rejected, not deferred**: first-party
clients speak 1.3, Go 1.26's PQ KEMs only attach to 1.3, and a dedicated
phase makes a production revert a one-commit `git revert` rather than a
reason to leave the floor on 1.2.

`runtime/secret` and `encoding/json/v2` stay off this toolchain.

* Implementation Plan: [0142-PLAN-mcrelay-2026-09-public-edge-audit.md](./0142-PLAN-mcrelay-2026-09-public-edge-audit.md)

## Consequences

* Good, because unauthenticated keep-alives and unbounded accepts stop
  being a free fd/memory hold on the public edge.
* Good, because the remaining join-slot pairing order is tested and the
  durable `phones` counter stays honest without waiting for the sweeper.
* Good, because plaintext registration secrets become GC-eligible as soon
  as they have been hashed, instead of riding the control/splice goroutine
  for the session lifetime.
* Good, because TLS 1.3 + Go 1.26 PQ KEMs become the outer hop's floor,
  matching the inner hop's actual client capability.
* Good, because mcrelay finally gets the same class of `GOMEMLIMIT` and
  config-permission hygiene mcremote already has.
* Good, because a `trusted_proxies: ["0.0.0.0/0"]` foot-gun can no longer
  turn XFF into a rate-limit bypass, and IPv6 subscribers stop looking
  like thousands of distinct clients.
* Neutral, because the happy path (register → dial → tunnel → splice) is
  unchanged on the wire; F21 is the only operator-visible compatibility
  change, and it is isolated in its own phase.
* Neutral, because Green Tea GC and heap-base randomization are already
  in effect at Go 1.26.6 with no code change.
* Bad, because one pass still touches the serve/hub/TLS/CLI files (same
  mitigation as 0115: phased, independently revertible commits).
* Bad, because a TLS 1.3 floor will fail any leftover TLS 1.2 scanner or
  a hypothetical third-party client; first-party code is not that client.

## Pros and Cons of the Options

### O1 — one hardening pass including TLS 1.3 (chosen)

* Good, because overlapping diffs are reviewed once.
* Good, because F21 is its own phase, so a production problem reverts
  without rolling back IdleTimeout / the F18 test.
* Good, because tests land with the code they pin, including the missing
  reverse-order F18 case.
* Bad, because the pass is larger than bugs-only.

### O2 — P1/P2 only

* Good, because the smallest production diff.
* Bad, because F21 stays a known public-edge TLS 1.2 surface for another
  cycle after the owner asked for rock-solid.
* Bad, because `go fix` drift (F24) and the coverage floor (F23) will be
  rewritten again the next time anyone touches `fileconfig.go` / tests.

### O3 — record only

* Good, because zero risk now.
* Bad, because F17 is an unauthenticated keep-alive hold on a public
  port; documenting it without fixing it is the worst of both.

### O4 — O1 without TLS 1.3

* Good, because it avoids the only operator-visible compatibility change.
* Bad, because that is the same reason 0115 deferred it, and the
  toolchain has since put PQ KEMs behind the 1.3 floor; deferring again
  is a decision to not take a free cryptographic upgrade.

## Confirmation

* `go vet` and `go test -race -count=1 ./internal/relay/... ./internal/relayhost/...`
  stay green.
* New regression tests demonstrably fail against the pre-fix tree for:
  * F18 — `abandonTunnel` then `phoneGone` with a canary join: `phones`
    stays at 1, not 0.
  * F17 — an HTTP keep-alive to `/healthz` is closed by `IdleTimeout`
    (or `Connection: close`) rather than held indefinitely.
  * F19 — with `max_conns=1` and one live HTTP connection, a second
    `Dial` with a 200 ms timeout fails (the cap **blocks** `Accept`, it
    does not RST).
  * F22 — serve refuses a group/other-readable config file that contains
    hosts; ACME cache and files-mode TLS keys are owner-only or refused.
  * F21 — a TLS 1.2-only dial is rejected; 1.3 succeeds.
  * F35 — `trusted_proxies` containing `0.0.0.0/0` or `::/0` is rejected
    at Validate.
  * F36 — two IPv6 addresses in the same /64 share one accept/join
    window (test with synthetic RemoteAddr).
  * F39 — `parseAllowParts` error text does not contain the secret.
* `go test -cover` reports ≥ 80% for `internal/relay` and
  `internal/relayhost` (`scripts/coverage-delta.sh floor --minimum 80.0`).
* Wire compatibility: `internal/relay/e2e_test.go` passes unmodified —
  no envelope, code, or close-reason visible to current clients changes.
* `govulncheck ./...` stays at 0 called vulnerabilities.
* `make pre-add-check` clean on every staged Go file.
* After deploy: a live register / join / splice / SIGTERM drain against
  the production relay, using the `d4_live_test.go` pattern.

## Amendment — plan lock (2026-09-05)

The companion plan was rewritten so an implementer has **one** path per
finding. These choices were left open in the first draft and are now
locked; they do not change O1.

| # | Was open | Locked |
|---|---|---|
| L1 | `ReadTimeout` “if a test proves it is safe” | **Leave 0**, matching mcremote. Hijacked splices must not grow a global read deadline. |
| L2 | `LimitListener` before vs after TLS; `x/net/netutil` vs local | **Before TLS** in `ListenAndServe`. Local `limitedListener` in `internal/relay/conns.go`. Do not add a direct `golang.org/x/net` require. Cap **blocks** `Accept` (semaphore), does not RST. |
| L3 | Clear `fc.Hosts` vs replace the slice | **In-place** `h.Secret = ""`. Keep the slice length. |
| L4 | Share `applyMemoryLimit` with `internal/cli` vs copy | **Copy** into `internal/relay/memlimit.go`. Default **512 MiB**. Do not import `internal/cli`. Tests cover `memoryLimitPlan` only — never `debug.SetMemoryLimit` in a test. |
| L5 | `FileIsOwnerOnly` in `Validate` vs `Load` vs serve | **`Load`** when `ConfigFile != ""`; **`checkSecretFiles`** in serve for TLS PEMs; **`EnsurePrivateDir`** on the ACME cache from `ApplyTLS` (HTTP-01 only). Do not change `certs.EnsureACME` (mcremote DNS-01). |
| L6 | Raise MinVersion in `certs.ACMEBundle.TLSConfig` vs clone | **Clone in `ListenAndServe` only.** Always set `MinVersion = tls.VersionTLS13` on that clone (override a 1.2 input). `certs` stays 1.2 for mcremote. |
| L7 | certmagic solver `ReadHeaderTimeout` if a knob exists | **No knob** in certmagic v0.25.4 `httpSolver.serve`. Do not fork. Comment + ops paragraph only. |
| L8 | Share `shutdownSignals` vs copy the restorer | **Copy the restorer.** Switch `signal.NotifyContext` to `cmd.Context()`. Do not invent a package. |
| L9 | F27 / F34 “unless the diff is tiny” | **Skip both.** F29 on `relayhost` skipped (not this binary). F30 clears **splice** pool only. |
| L10 | F28 `http.Protocols` vs ALPN strip only | **Both.** `New` sets `http.Server.Protocols` to HTTP/1 only; `stripHTTP2` stays. |
| L11 | F39 quote host_id vs generic | Generic: `allow: want host_id:secret` with **no** `%q` of the raw input. |
| L12 | F36 canonicalize in `clientIP` vs `allowRateRetry` | **`rateIPKey` at every `clientIP` return** (rate is the only caller). IPv4 `/32` via `To4()`; IPv6 mask lower 64 bits. Trusted-proxy `Contains` still uses the unmasked IP **before** `rateIPKey`. |

F38 remains deferred (wire-visible). `GOEXPERIMENT=goroutineleakprofile` on `make debug` is **kept**. `runtime/secret` and `encoding/json/v2` are **not added**.

## More Information

* Audit basis: full read of `cmd/mcrelay` + `internal/relay` + the ACME
  HTTP-01 path in `internal/certs` on 2026-09-05, Go 1.26.6,
  `coder/websocket v1.8.15`. `go test -cover` and `go fix -diff
  ./internal/relay/` run in this session. `govulncheck ./...` clean.
* Prior art: [0015](./0015-MADR-mcrelay-transport-security.md),
  [0016](./0016-MADR-mcrelay-audit-hardening.md),
  [0017](./0017-MADR-mcrelay-memory-security-action-plan.md),
  [0115](./0115-MADR-mcrelay-go126-audit-and-hardening.md);
  0068 P1/P6, 0091 D3/D5/D10, 0138 `applyMemoryLimit`.
* Go 1.26: [release notes](https://go.dev/doc/go1.26) — Green Tea GC
  default, heap-base randomization, `crypto/tls` PQ KEMs, `go fix`
  modernizers, experimental `runtime/secret` (not adopted).
* 0017 E5 (metrics) remains deferred.
* Ops: `docs/ops-mcrelay.md`; production relay
  `wss://headscale.lallygag.net:8443`.

## Observed — execution results (2026-09-05)

Executed as `0142-PLAN` Phases 1–8. Each phase gated on `make pre-add-check`,
`go vet`, and `go test -race -count=1 ./internal/relay/... ./internal/relayhost/...`.
`internal/relay/e2e_test.go` hash stayed
`a2d6df96ba5d509af0476e9d16e893966ff4b891`.

* **Coverage**: `internal/relay` **80.0385%** (1247/1558);
  `internal/relayhost` **81.2183%** (160/197)
  (`coverage-delta.sh floor --minimum 80.0` → `pass`).
* **Skipped as locked**: F27 (setup-service flag fork), F34 (synctest),
  F29 on `relayhost`, F33 XFF `slices.Backward`. F38 deferred (wire-visible).
* **Kept**: `make debug` `GOEXPERIMENT=goroutineleakprofile`. Not added:
  `runtime/secret`, `encoding/json/v2`.
* **`go fix -diff ./internal/relay/`**: empty after Phase 7.
* Live production smoke (`d4_live_test.go` pattern) is an operator step
  after deploy, not this execution.
