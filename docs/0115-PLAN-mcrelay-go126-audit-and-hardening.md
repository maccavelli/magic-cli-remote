---
status: completed
date: 2026-08-25
associated-madr: "0115-MADR-mcrelay-go126-audit-and-hardening.md"
owner: [Project Owner]
target-milestone: "mcrelay hardening pass"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Implement the mcrelay 2026-08 hardening pass

Associated MADR: [0115-MADR-mcrelay-go126-audit-and-hardening.md](0115-MADR-mcrelay-go126-audit-and-hardening.md)

## Goal

Close findings F1–F16 of the 0115 audit in one reviewed pass: fix the join
lifecycle race, pre-auth frame sizing, secret-parsing mismatch, rate-map
eviction, host-side deadlines and frame-limit coupling; retire dead code;
canonicalize the error vocabulary and envelope I/O; adopt the Go 1.24–1.26
idioms the code predates; and raise `internal/relay` and `internal/relayhost`
to ≥ 80.0% statement coverage — all with zero change visible to existing
phones and hosts on the wire.

## Scope

### In scope (the only files any phase may touch)

* `internal/relay/`: `server.go`, `hub.go`, `config.go`, `clientip.go`,
  `fileconfig.go`, `cli.go`, and its `*_test.go` files (existing and new).
* `internal/relayhost/`: `client.go` and its `*_test.go` files.
* `internal/config/config.go`, `internal/config/load.go`,
  `internal/daemon/daemon.go` — **only** the F6 knob
  (`relay.max_frame_bytes`) and its wiring.
* `configs/config.example.yaml`, `docs/config.md`, `docs/config-mcrelay.md`,
  `docs/ops-mcrelay.md` — documentation for the F6 knob and changed
  operational behaviour.

### Out of scope

TLS 1.3 floor (MADR O4, deferred); Prometheus metrics (0017 E5); any change
to envelope JSON shapes, message type strings, wire error-code strings, or
close-reason strings observable by current clients; protocol-v1;
`cmd/mcrelay/main.go`; the Flutter app. Anything discovered mid-execution
outside this list stops and waits per `AGENTS.md` and the global
plan-deviation rule.

## Stability rule

Every phase ends with `make pre-add-check` on its staged Go files,
`go vet ./internal/relay/... ./internal/relayhost/...`, and
`go test -race -count=1 ./internal/relay/... ./internal/relayhost/...` green,
then one commit. The existing e2e suite (`internal/relay/e2e_test.go`) must
pass **unmodified in every phase**; it is the wire-compatibility oracle. No
push at any point.

## Cross-cutting contracts

* **Error vocabulary (P1).** The wire strings are frozen exactly as today:
  `limit`, `host_offline`, `unknown_session`, `unauthorized`, `internal`,
  `timeout`, `bad_payload`, `bad_frame`, `rate_limited`. P1 introduces
  sentinels; every later phase must route new failure paths through them.
* **Lock discipline.** All `pendingJoin` lifecycle state (`pending` map
  membership, the new `phoneGone` flag, slot release) is guarded by `hub.mu`
  only. `ready` remains buffer-1; `done` remains close-once via `doneOnce`.
* **Read-limit ladder.** Every WS connection starts at
  `ControlReadLimitBytes` (64 KiB) until its plane is authenticated/joined;
  `splice()` alone raises both sides to `Limits.MaxMessageBytes`
  (`server.go:854–855` today, unchanged).

## Dependency and delivery order

P1 (errors) → P2 (join lifecycle) → P3 (pre-auth limits) → P4 (rate map) →
P5 (config parsing + flag plumbing) → P6 (relayhost) → P7 (idioms) →
P8 (coverage + docs + final regression). P3–P5 are independent of each other
but land in this order to keep diffs reviewable; P7 must follow P2–P6 so the
idiom sweep does not conflict with functional diffs.

## Implementation Steps

### P1 — Canonical error vocabulary (F7, F8)

**Outcome.** Hub and server communicate failures through sentinel errors;
wire strings are emitted from one mapping; dead code is gone. No wire change.

**Files.** `internal/relay/hub.go`, `internal/relay/server.go`,
`internal/relay/errors.go` (create), `internal/relay/errors_test.go`
(create), `internal/relay/hub_test.go`, `internal/relay/server_test.go`,
`internal/relay/rate_test.go` (only if it calls `allowAccept`).

**Steps.**

1. Create `internal/relay/errors.go`:

   ```go
   package relay

   import "errors"

   // Join-plane failure sentinels (0115 F8). The wire code for each is the
   // string in errCode — frozen; changing one is a protocol change.
   var (
       errLimit          = errors.New("limit")
       errHostOffline    = errors.New("host_offline")
       errUnknownSession = errors.New("unknown_session")
       errUnauthorized   = errors.New("unauthorized")
       errInternal       = errors.New("internal")
   )

   // errCode maps a hub error to its frozen wire code.
   func errCode(err error) string {
       switch {
       case errors.Is(err, errLimit):
           return "limit"
       case errors.Is(err, errHostOffline):
           return "host_offline"
       case errors.Is(err, errUnknownSession):
           return "unknown_session"
       case errors.Is(err, errUnauthorized):
           return "unauthorized"
       default:
           return "internal"
       }
   }
   ```

2. In `hub.go`, replace every `fmt.Errorf("<code>")` return with the matching
   sentinel: `register` (`limit`), `beginJoin` (`host_offline`, `limit`,
   `internal`), `claimTunnel` (`unknown_session`, `unauthorized`),
   `writeControl` (`host_offline`). Drop the now-unused `fmt` import if
   nothing else uses it.
3. In `hub.register`, delete the unreachable `unknown_host` branch
   (`hub.go:107–109`) and replace it with `errUnauthorized` plus the comment
   `// Defense in depth: unreachable via handleHost (checkSecret gates).`
   (F7 — keeps the check, removes the never-sent distinct code).
4. In `server.go`, replace every `err.Error()` used as a wire code or close
   reason with `errCode(err)`: `handleHost` register-failure path
   (`server.go:548–549`), `handlePhone` beginJoin-failure path (line 663:
   `code := errCode(err)`, and the `if code == "limit"` comparison becomes
   `errors.Is(err, errLimit)`), `handleTunnel` claim-failure path
   (`server.go:786–787`).
5. Delete `Server.allowAccept` (`server.go:412–415`); update any test that
   called it to call `s.allowRateRetry(ip, rateBucketAccept, max)` directly.
6. Delete the unused `hostID` field from `hostSlot` (`hub.go:40`) and its
   assignment in `register` (`hub.go:126`).
7. Create `errors_test.go`: table test asserting `errCode` over all five
   sentinels plus a wrapped sentinel (`fmt.Errorf("x: %w", errLimit)`) and an
   unknown error → `internal`.

**Verification.** Stability rule; plus
`go test -run 'TestHub|TestServer|TestErrCode' -race -count=1 ./internal/relay/`
and one grep gate:
`! grep -n 'err.Error()' internal/relay/server.go | grep -v slog` (error
strings may reach logs, never the wire).

### P2 — Join lifecycle: close the timeout/claim race (F1)

**Outcome.** Whichever of {phone leaves, tunnel publishes} happens first, the
other side observes it under `hub.mu`: the phone slot is released exactly
once, the tunnel is never parked, and `handleTunnel` never waits on a `done`
that no one will close.

**Files.** `internal/relay/hub.go`, `internal/relay/server.go`,
`internal/relay/hub_test.go`, `internal/relay/join_race_test.go` (create).

**Steps.**

1. Add field `phoneGone bool` to `pendingJoin` (guarded by `hub.mu`; document
   that in the struct comment).
2. Add to `hub.go`:

   ```go
   // phoneGone is called by handlePhone when it abandons a join (timeout or
   // request-context cancel). It resolves the race with claimTunnel /
   // publishTunnel under one lock (0115 F1). The returned conn, when non-nil,
   // is an already-published tunnel the caller must close (outside the lock).
   func (h *hub) phoneGone(p *pendingJoin) (orphan *websocket.Conn) {
       h.mu.Lock()
       defer h.mu.Unlock()
       if _, ok := h.pending[p.sessionID]; ok {
           // Not yet claimed: identical to the old cancelJoin path.
           delete(h.pending, p.sessionID)
           h.releasePhoneLocked(p.hostID)
           close(p.ready)
           return nil
       }
       // Claimed. Either the tunnel is already in ready, or publishTunnel /
       // abandonTunnel has yet to run and will observe phoneGone.
       p.phoneGone = true
       select {
       case t := <-p.ready:
           h.releasePhoneLocked(p.hostID)
           p.closeDone()
           return t
       default:
           return nil
       }
   }
   ```

3. In `publishTunnel`, before the send, add:

   ```go
   if p.phoneGone {
       h.releasePhoneLocked(p.hostID)
       p.closeDone()
       return false
   }
   ```

4. In `abandonTunnel`, add `p.closeDone()` after the release, and guard the
   release: if `p.phoneGone` is already set, `phoneGone` deferred the release
   to this path, so releasing here stays exactly-once — add the comment
   spelling that out (release happens here in both cases; the guard is
   analytical, not conditional code).
5. In `handlePhone`, replace `s.hub.cancelJoin(pending.sessionID)` in the
   timeout arm and the ctx-done arm (`server.go:701–709`) with:

   ```go
   if orphan := s.hub.phoneGone(pending); orphan != nil {
       _ = orphan.Close(websocket.StatusGoingAway, "phone_gone")
   }
   ```

   The timeout arm keeps its `timeout` error write and log line; the ctx arm
   keeps its bare return.
6. `cancelJoin` remains for `expireStalePending` and the dial-write-failure
   path (both operate strictly on pending joins); add a comment restricting
   it to pending-only callers.
7. Create `join_race_test.go` with a deterministic interleaving harness
   (direct hub calls, no sockets):
   * `TestPhoneGoneAfterClaimBeforePublish` — `beginJoin`, `claimTunnel`,
     `phoneGone` (returns nil), then `publishTunnel` returns false,
     `phoneCount == 0`, `done` closed.
   * `TestPhoneGoneAfterPublish` — `beginJoin`, `claimTunnel`,
     `publishTunnel` true, then `phoneGone` returns the tunnel conn,
     `phoneCount == 0`, `done` closed.
   * `TestPhoneGonePendingIsCancel` — `beginJoin`, `phoneGone` → `ready`
     closed, `phoneCount == 0`, later `claimTunnel` → `errUnknownSession`.
   * `TestAbandonAfterPhoneGone` — `beginJoin`, `claimTunnel`, `phoneGone`,
     `abandonTunnel` → `phoneCount == 0` (exactly-once release), `done`
     closed.
   Each test must FAIL against the pre-P2 tree (verify once by `git stash` of
   the hub/server diff; record the observed failure in the commit message).
8. Extend `hub_reconcile_test.go` expectation if needed: with F1 fixed, the
   sweep should find zero divergence in these interleavings — assert that.

**Verification.** Stability rule; `go test -race -count=1 -run
'TestPhoneGone|TestAbandon|TestJoin' ./internal/relay/` green; e2e suite
unmodified and green.

### P3 — Pre-auth read limits (F2)

**Outcome.** All three planes read their first (unauthenticated) envelope
under the 64 KiB control limit; only `splice()` raises limits, after auth.

**Files.** `internal/relay/server.go`, `internal/relay/server_test.go`.

**Steps.**

1. `handlePhone` (`server.go:623`): change
   `conn.SetReadLimit(int64(s.cfg.Limits.MaxMessageBytes))` to
   `conn.SetReadLimit(int64(ControlReadLimitBytes))`, with comment
   `// D16 extended (0115 F2): control limit until join_ok; splice raises it.`
2. `handleTunnel` (`server.go:754`): same change, comment referencing
   tunnel_ok.
3. Add `TestPreAuthFrameCapPhone` and `TestPreAuthFrameCapTunnel` to
   `server_test.go`: connect over httptest, send a text frame of
   `ControlReadLimitBytes+1` bytes as the first message, assert the
   connection closes with a read-limit failure and no join/claim occurred.
   Assert also that a legitimate join followed by a 200 KiB spliced frame
   still passes (guards against capping the post-auth path) — reuse the e2e
   splice harness helpers.

**Verification.** Stability rule; both new tests fail pre-change (limit was
1 MiB, the oversized frame was accepted); e2e green.

### P4 — Rate limiter: oldest-first eviction and struct keys (F4, F13)

**Outcome.** Capacity pressure evicts the oldest window deterministically;
per-check key allocation is gone. Behaviour at and below capacity is
byte-identical to today.

**Files.** `internal/relay/server.go`, `internal/relay/rate_test.go`.

**Steps.**

1. Replace the map key type: `rate map[string]*rateWindow` →
   `rate map[rateKey]*rateWindow` with
   `type rateKey struct{ bucket, id string }`; delete the `"\x00"` concat
   (`server.go:436`).
2. Add `func (s *Server) evictOldestRateLocked()`: single linear scan for the
   smallest `start`; delete that key. Replace both random-eviction loops
   (`allowRateRetry` insertion branch `server.go:446–451`, `pruneRateLocked`
   hard-cap loop `server.go:469–475`) with TTL prune first, then
   `evictOldestRateLocked` while over cap.
3. `rate_test.go`: add `TestRateEvictionOldestFirst` — fill to `rateMapMax`
   with staggered `start` times (inject via direct map writes under the test's
   own lock access, same package), insert one more key, assert the oldest key
   was the one evicted and a hot window's count survived. Update any test
   naming the old string key form.

**Verification.** Stability rule;
`go test -race -count=1 -run TestRate ./internal/relay/` green; the
oldest-first test fails pre-change (random eviction makes it flaky by
construction — assert deterministically on the post-change invariant, and
document in the test comment why pre-change behaviour cannot satisfy it).

### P5 — Config parsing and flag plumbing (F3, F11, F15)

**Outcome.** One parse for `--allow`/`MCRELAY_HOSTS` entries; `fs.ErrNotExist`
detection; one flag-application mechanism; one string-list expander.

**Files.** `internal/relay/config.go`, `internal/relay/fileconfig.go`,
`internal/relay/cli.go`, `internal/relay/fileconfig_test.go`.

**Steps.**

1. In `config.go`, add
   `func parseAllowParts(s string) (id, secret string, err error)` containing
   the current `ParseAllowFlag` logic (trim whole string once, split on first
   `:`, validate id, `len(secret) >= 16`), returning the **trimmed-string**
   secret slice. Reimplement `ParseAllowFlag` as
   `id, secret, err := parseAllowParts(s); … HashSecret(secret)`.
2. In `fileconfig.go`, reimplement `hostEntryFromAllow` on `parseAllowParts`
   — delete the raw-string re-slice (`fileconfig.go:760–767`). (F3)
3. Replace `isNotExist` (`fileconfig.go:782–784`) with
   `errors.Is(err, fs.ErrNotExist)` at its call site and delete the helper;
   add `io/fs` and `errors` imports. (F11)
4. Delete `expandDomainList` (`cli.go:348–359`); its call site uses
   `expandStringList`. (F15)
5. In `newServeCmd` RunE (`cli.go:216–266`), delete every manual override
   whose key `bindRelayFlags` already binds (`listen-host`, `listen-port`,
   `data-dir`, `tls-mode`, `tls-cert`, `tls-key`, `tls-domain`, `tls-email`,
   `tls-acme-directory`, `tls-acme-staging`, `tls-acme-http-port`,
   `tls-acme-challenge`, `tls-route53-zone-id`, `tls-route53-region`,
   `tls-route53-profile`, `allow-legacy-tunnel-secret`). Keep only:
   `trusted-proxy` (StringArray, deliberately post-Load), `--allow-plaintext`
   (runtime-only), and the `logLevel`/`logFormat` persistent-flag overrides.
   Delete the now-unused local variables. (F15)
6. `fileconfig_test.go`: add
   * `TestAllowEntryTrailingWhitespaceSecret` — `--allow "h1:0123456789abcdef "`
     (trailing space): stored `HostEntry.Secret` equals the trimmed secret,
     and `HashSecret` of it matches `ParseAllowFlag`'s hash (fails
     pre-change);
   * `TestServeFlagsSingleMechanism` — for three representative flags
     (`--listen-port`, `--tls-mode`, `--allow-legacy-tunnel-secret`), load
     via `Load` with a `pflag` set marked Changed and assert the resolved
     `FileConfig` reflects them with the manual overrides gone (guards the
     P5.5 deletion).

**Verification.** Stability rule; `go test -race -count=1 -run
'TestFileConfig|TestAllow|TestServeFlags|TestListen' ./internal/relay/`
green; `internal/relay/listen_policy_test.go` and
`setup_service_refresh_test.go` untouched and green.

### P6 — relayhost: deadlines, frame-limit coupling, shared envelope I/O (F5, F6, F14, F10-host)

**Outcome.** The register exchange is bounded; the bridge read limit follows
configuration; envelope I/O exists once, with one version-check behaviour;
bridge goroutines use `WaitGroup.Go`.

**Files.** `internal/relay/protocol.go`, `internal/relay/server.go`,
`internal/relayhost/client.go`, `internal/relayhost/client_test.go`,
`internal/config/config.go`, `internal/config/load.go`,
`internal/daemon/daemon.go`, `configs/config.example.yaml`,
`docs/config.md`.

**Steps.**

1. In `protocol.go`, add exported canonical I/O (F14):

   ```go
   // WriteEnvelope / ReadEnvelope are the canonical join-plane frame I/O for
   // both mcrelay and relayhost (0115 F14). ReadEnvelope enforces text
   // framing and rejects unsupported envelope versions.
   func WriteEnvelope(ctx context.Context, c *websocket.Conn, env Envelope) error
   func ReadEnvelope(ctx context.Context, c *websocket.Conn) (Envelope, error)
   ```

   Bodies are moved verbatim from `server.go` `writeEnv`/`readEnv`
   (`server.go:953–977`) including the version check. `server.go` keeps thin
   unexported wrappers (`writeEnv = WriteEnvelope` call-through) so its call
   sites do not churn, or call sites are updated — choose call-site update
   (mechanical, ~15 sites) and delete the wrappers.
2. In `relayhost/client.go`, delete local `writeEnv`/`readEnv`
   (`client.go:410–431`) and call `relay.WriteEnvelope`/`relay.ReadEnvelope`.
   The host thereby gains the version check — intended (F14 drift closed);
   note it in the commit message.
3. Bound the register exchange (F5): in `session`, wrap dial + register write
   + register read in `hctx, hcancel := context.WithTimeout(ctx, 30*time.Second)`
   released after `register_ok`; the post-register control read loop stays on
   the undeadlined `ctx`. 30 s matches `openTunnel`'s existing bound
   (`client.go:219`).
4. Frame-limit knob (F6):
   * `internal/config/config.go`: add to `RelayConfig`
     `MaxFrameBytes int `mapstructure:"max_frame_bytes"`` with doc comment
     `// MaxFrameBytes caps a relayed WS frame on the host bridge. Must match
     the relay's limits.max_message_bytes when that is raised. 0 = 1 MiB.`;
     in the relay validate block (`config.go:1380` area) accept `0` or
     `4096 ≤ v ≤ 16<<20`, else error.
   * `internal/config/load.go`: `v.SetDefault("relay.max_frame_bytes", 0)`
     and `v.BindEnv("relay.max_frame_bytes", "MCREMOTE_RELAY_MAX_FRAME_BYTES")`
     beside the existing relay keys (`load.go:62–65`, `368–371`).
   * `internal/relayhost/client.go`: add `MaxFrameBytes int` to
     `relayhost.Config`; in `bridge`, replace the constant with
     `limit := cfg… ; if limit <= 0 { limit = bridgeWSReadLimit }` — plumb
     via a `Client` field set in `New`.
   * `internal/daemon/daemon.go:580`: pass
     `MaxFrameBytes: cfg.Relay.MaxFrameBytes`.
   * `configs/config.example.yaml`: add the key with its comment under
     `relay:` (template parity test will enforce the live-config gap is
     surfaced, not auto-fixed — note this for the operator).
     **Deviation (2026-08-25, owner-approved):** the parity gate this phase
     must pass also enforces the key in three companion templates absent from
     this phase's file list — `internal/cli/service/defaults_mcremote.yaml`,
     `configs/config.prod.example.yaml`, and `configs/config.mesh-grok.yaml`.
     Owner chose "add to all four"; those three files join P6's scope.
5. Bridge goroutines → `wg.Go` (F10 host side): replace the two
   `wg.Add(…)`/`go func` pairs in `bridge` with `wg.Go(func(){…})`; the
   recover guards and `defer cancel()` stay inside the closures; delete the
   now-redundant `wg.Add(2)` and `defer wg.Done()` lines.
6. `client_test.go` additions:
   * `TestRegisterExchangeDeadline` — a listener that accepts the WS upgrade
     and then never replies: with the 30 s bound shortened via a test hook
     (add `var registerTimeout = 30 * time.Second` package var, overridden in
     the test), `session` returns within the bound (fails pre-change: hangs
     until the test's own watchdog).
   * `TestBridgeFrameLimitFollowsConfig` — bridge with
     `MaxFrameBytes: 4096`: a 5 KiB frame kills the tunnel; with `0` the
     1 MiB default applies.
   * `TestEnvelopeVersionRejected` — a control frame with `v: 99` is refused
     by the shared reader (documents the F14 behaviour change).

**Deviation (2026-08-25, owner-approved) — bridge TCP leg unblocked on WS
death.** Writing this phase's frame-limit test deadlocked and exposed a
pre-existing gap, F1's host-side cousin: `bridge`'s TCP leg blocks in
`tcp.Read`, which context cancellation cannot interrupt, so when the WS side
died first the leg stayed parked until the daemon closed the local conn
(daemon read deadlines bound this in production; `Run`'s 15 s drain only
warns). Owner chose "fix now in P6": `context.AfterFunc(ctx, tcp.Close)` in
`bridge` unblocks the leg the instant the bridge context dies, and
`TestBridgeUnblocksTCPLegOnWSDeath` pins it — pre-fix, that scenario
deadlocks against a `net.Pipe` peer. Files already in this phase's list.

**Verification.** Stability rule **plus**
`go test -race -count=1 ./internal/config/... ./internal/daemon/...` and
`go test -run TestTemplateParity ./internal/cli/...` (template parity test
must see the new example key); e2e green.

### P7 — Idiom sweep (F9, F10-relay, F12)

**Outcome.** Mechanical modernization; zero behaviour change.

**Files.** `internal/relay/server.go`, `internal/relay/config.go`,
`internal/relayhost/client.go`, `internal/relay/splice_bench_test.go`.

**Steps.**

1. `min`/`max` builtins (F9): sweeper bounds (`server.go:296–303`), retry max
   (`server.go:653–656`), idle tick clamp (`server.go:872–878`), `clampInt`
   body (`config.go:138–146` — keep the function, one-line body
   `return min(max(v, lo), hi)`), reconnect backoff cap (`client.go:132–134`).
2. `sync.WaitGroup.Go` for the splice `copyDir` pair
   (`server.go:933–935`): `wg.Go(func() { copyDir(a, b) })` etc.; remove
   `wg.Add(2)` and the `defer wg.Done()` inside `copyDir`.
3. `splice_bench_test.go:108`: `for i := 0; i < b.N; i++` → `for b.Loop()`.
4. Grep gates (run, and record output in the commit message):
   `! grep -rn 'wg.Add(' internal/relay/server.go internal/relayhost/client.go`
   and `! grep -n 'b\.N' internal/relay/splice_bench_test.go`.

**Verification.** Stability rule; `go test -bench BenchmarkSplice -benchtime
1x ./internal/relay/` runs; benchmark numbers before/after recorded in the
commit message (expect noise-level delta only).

### P8 — Coverage to the floor, docs, final regression (F16)

**Outcome.** `internal/relay` ≥ 80.0%, `internal/relayhost` ≥ 80.0%
(per-package floors — per the MADR, no per-function floor; `runSetupService`
may stay cold if the package clears the bar), docs updated, full suite green.

**Files.** New/extended tests only:
`internal/relay/server_lifecycle_test.go` (create),
`internal/relay/cli_test.go` (create), `internal/relay/fileconfig_test.go`,
`internal/relayhost/client_test.go`; docs: `docs/config-mcrelay.md`,
`docs/ops-mcrelay.md`.

**Steps.**

1. Target the measured cold spots with deterministic unit tests:
   * `handleTunnel` (53.7%): bad first-envelope type, bad payload, invalid
     host/session ids, claim of unknown session, tunnel_ok write failure
     (close the phone side first) — each asserting the frozen wire code.
   * `handleHost` (59.6%): non-register first envelope, invalid host_id,
     register rate-limit denial (bucket pre-filled via `allowRateRetry`),
     wrong secret, register replacement (second register closes the first
     control with `replaced`).
   * `pingHostControl` (43.8%): ping failure path via a closed conn → `fail`
     cancel observed.
   * `ListenAndServe` (56.5%): files-mode TLS with the `tls_test.go`
     fixtures; plaintext-refusal branch; loopback allowance.
   * `tlsHandshakeLogFilter.Write` (0%): scanner line → Debug, other → Warn,
     empty → no-op (capture via `slog` test handler).
   * `fileconfig.go` 0% helpers: `RecomputePaths`, `ConfigPathHint`,
     `DataDirHint`, `EnsureDataDir` (tempdir), `expandStringList` edge cases.
   * `cli_test.go`: execute the cobra tree in-process for `version`,
     `paths --json` (against a temp config), and `serve` with an invalid
     config (error path only — no listener started).
2. Measure: `go test -count=1 -coverprofile=cov.out ./internal/relay/ &&
   go tool cover -func=cov.out | tail -1` — repeat for `relayhost`; iterate
   step 1 until both totals ≥ 80.0%. Do not add tests solely to inflate
   trivial accessors; failure paths first.
3. Docs:
   * `docs/config-mcrelay.md`: note the pre-auth 64 KiB first-frame bound and
     oldest-first rate eviction (operational behaviour, not knobs).
   * `docs/config.md`: document `relay.max_frame_bytes`.
   * `docs/ops-mcrelay.md`: one paragraph on the F1 fix (parked-tunnel
     symptom retired; slot-sweep WARN lines for this cause should no longer
     appear).
4. Final regression, in order:

   ```sh
   go vet ./...
   go build ./...
   go test -race -count=1 ./internal/relay/... ./internal/relayhost/... \
     ./internal/config/... ./internal/daemon/... ./internal/cli/...
   go test -count=1 ./...                       # full module
   scripts/coverage-snapshot.sh --output /tmp/0115-after \
     --go ./internal/relay --go ./internal/relayhost
   scripts/coverage-delta.sh floor --after /tmp/0115-after --minimum 80.0 \
     --go ./internal/relay --go ./internal/relayhost
   ```

5. Update the 0115 MADR: `status: proposed → accepted` (owner call at
   review), append an Observed section recording final coverage numbers and
   the benchmark delta from P7.

**Verification.** Step 4 all green; `coverage-delta.sh floor` exits 0.

## Verification (whole plan)

* Every phase's stability rule passed at its commit.
* All new regression tests listed in P2/P3/P4/P5/P6 demonstrably failed
  against the pre-phase tree (each phase's commit message names which).
* e2e suite (`e2e_test.go`) never modified, green in every phase.
* Final: `coverage-delta.sh floor --minimum 80.0` green for both packages;
  full-module `go test` green; `make pre-add-check` clean on every staged Go
  file across all commits.

## Rollout and Rollback

**Rollout.** Land as 8 commits on `master` (no push without an explicit ask).
Deploying to the production relay (`headscale.lallygag.net`) is a separate
operator action: `mcrelay update` on the relay host after a release is cut;
the F6 knob requires no config change (default 0 preserves today's 1 MiB).
After deploy, watch `journalctl` for: absence of `phone slot divergence
corrected` WARNs attributable to join timeouts, and normal
`host_registered` / `splice ended` cadence. The host side (`mcremote`) picks
up F5/F6/F14 on its own next release; old host ↔ new relay and new host ↔
old relay both remain compatible (wire strings frozen, version check accepts
`v:1` from all shipped peers).

**Rollback.** Each phase is one commit and independently revertible;
`git revert` in reverse order for a full unwind. No migrations, no persisted
state, no config schema breakage (the F6 knob is additive; reverting P6
leaves an unknown-but-ignored YAML key at worst — viper ignores unknown
keys). A production relay rollback is `mcrelay update --force` to the prior
release.
