---
status: accepted
date: 2026-08-25
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Remediate the mcrelay 2026-08 audit findings in one hardening pass

## Context and Problem Statement

The owner asked for a fresh assessment of the mcrelay daemon: adherence to Go
1.26.5 idioms and standards, security stance, memory management, code
efficiency, bugs, gaps, incomplete wiring, and canonicalization/hardening
opportunities. mcrelay last received dedicated security work in
[0015](./0015-MADR-mcrelay-transport-security.md) (transport model),
[0016](./0016-MADR-mcrelay-audit-hardening.md) (R1–R39 audit), and
[0017](./0017-MADR-mcrelay-memory-security-action-plan.md) (memory/DoS, D7–D16,
E1–E3), with later touches from 0068 P1/P6 (first-envelope timeout, slot sweep,
retry hints) and 0091 (header caps, h2 strip, plaintext refusal). The toolchain
is now Go 1.26.5 (`go.mod`), `coder/websocket v1.8.15`.

The audited surface is the whole daemon: `cmd/mcrelay/main.go` (26 lines) and
`internal/relay/` (`server.go` 998, `hub.go` 426, `fileconfig.go` 807,
`cli.go` 531, `config.go` 266, `clientip.go` 95, `tls.go` 140, `protocol.go`
95, `update.go` 53), plus the host-side client `internal/relayhost/client.go`
(438). `go vet` is clean; `go test -cover` passes at **66.4%**
(`internal/relay`) and **71.4%** (`internal/relayhost`).

The question this record decides: **which of the audit's findings are worth
fixing, in what shape, and what is explicitly accepted as-is.**

### What the audit confirms is in good shape (facts, with evidence)

The prior MADRs' decisions are wired and load-bearing; none of the following
needs work:

* **Auth and enumeration resistance.** Allowlist secrets held as SHA-256
  (`config.go:200`), verified constant-time with equal work for unknown ids
  (`hub.go:92–102`); unknown host and wrong secret return the same
  `unauthorized` (`server.go:539–545`); unknown host_id on join is
  indistinguishable from offline (`hub.go:199–202`); single-use 32-byte
  tunnel tokens (`hub.go:316–322`), constant-time compared (`hub.go:251`);
  legacy secret-on-tunnel default-off (`fileconfig.go:489`).
* **Edge hardening.** TLS floor 1.2 with h2 ALPN stripped (`server.go:221–224`,
  `tls.go:121–133`); 16 KiB header cap vs the stdlib's 1 MiB
  (`server.go:168–170`); plaintext refused off-loopback without
  `--allow-plaintext` (`server.go:172–181`); WS origin policy rejects
  cross-origin browsers (`server.go:485–491`); first-envelope timeout kills
  upgrade-then-suspend peers (`server.go:145–166`); kernel keepalive 25/5/4 on
  both accept and dial legs (`server.go:206–213`, relayhost
  `client.go:347–355`).
* **Resource bounds.** Four rate buckets with per-IP and per-host keys
  (`server.go:52–58`), TTL prune in the background plus capacity prune on the
  hot path (`server.go:287–288`, `432–476`); limit ceilings rejected at config
  load and clamped in depth (`fileconfig.go:616–662`, `config.go:110–136`);
  durable phone-slot accounting that survives control reconnects
  (`hub.go:23–26`) with a two-sweep self-heal (`hub.go:367–412`); splice idle
  and lifetime watchdogs (`server.go:837–897`); shutdown drains hijacked
  splices and host controls (`server.go:262–284`).
* **Memory management.** Splice and bridge use streaming `Reader`/`Writer`
  with pooled 32 KiB buffers (`server.go:77–83`, `909–931`;
  `client.go:31–36`, `299–335`) — no per-frame allocation on the data path;
  the rate map is capacity-capped (`server.go:404–410`); logs truncate
  attacker-controlled ids (`config.go:242–266`). Every long-lived goroutine
  wears a recover-with-stack guard.

### Findings (facts, each verified against source)

**Bugs and gaps:**

* **F1 — join-timeout race parks a claimed tunnel and strands the phone slot
  until the sweep.** `handlePhone`'s timeout and ctx-done arms
  (`server.go:701–709`) call `cancelJoin` and return **without**
  `closeDone()`. If the host's `claimTunnel` has already removed the join from
  `pending` (`hub.go:265`), `cancelJoin` no-ops, and `publishTunnel`
  (`hub.go:272–282`) then succeeds into the buffered `ready` channel that no
  one will ever read. Consequences: the phone reservation is released by
  neither side (self-healed only by the 0068 P6 sweep after ~2×30 s), and
  `handleTunnel` blocks on `<-pending.done` (`server.go:809–812`) with no
  relay-side deadline — the connection is parked until the *host* gives up via
  downstream timeouts. The relay relies on peer behaviour to reclaim its own
  resources, which the rest of the design pointedly avoids.
* **F2 — pre-auth read limit on `/v1/phone` and `/v1/tunnel` is the splice
  limit, not the control limit.** 0017 D16 gave the host control plane a
  64 KiB `ControlReadLimitBytes` (`server.go:509`), but the phone and tunnel
  handlers set `MaxMessageBytes` — 1 MiB default, 16 MiB ceiling — **before**
  the first (unauthenticated) envelope (`server.go:623`, `754`). An
  unauthenticated peer can make the relay buffer a maximal frame per
  connection during the join window. The join/tunnel first envelopes are tiny
  JSON; D16's rationale applies to all three planes.
* **F3 — `--allow` / `MCRELAY_HOSTS` secrets keep surrounding whitespace that
  validation stripped.** `hostEntryFromAllow` (`fileconfig.go:760–767`)
  re-slices the *raw* string after `ParseAllowFlag` (`config.go:182–197`)
  validated a *trimmed* copy. A trailing space (easy in a systemd
  `Environment=` line) passes validation but is hashed into the stored secret,
  so the host can never authenticate — an infuriating silent mismatch.
* **F4 — rate-map capacity eviction removes a random key.** At `rateMapMax`
  the code deletes whatever map iteration yields first
  (`server.go:446–451`, `469–475`). An attacker who fills the map (4096 keys)
  can probabilistically evict *active* windows — including their own — and
  reset counters. Oldest-window eviction preserves the guarantee at the same
  cost.
* **F5 — relayhost register exchange has no deadline.** `session`
  (`client.go:140–181`) dials, writes `register`, and reads the reply on the
  root context, which has no deadline. Kernel keepalive reaps a *dead* relay
  in ~45 s, but a live-but-stalled relay (accepting TCP, never answering)
  wedges the reconnect loop indefinitely. `openTunnel` already does this right
  with a 30 s bound (`client.go:219`).
* **F6 — relayhost bridge read limit is hardcoded to the relay's *default*.**
  `bridgeWSReadLimit = 1 MiB` (`client.go:29`). A relay legitimately
  configured with a larger `max_message_bytes` (ceiling 16 MiB) sends frames
  the host-side bridge rejects, resetting tunnels. The limit should be a
  config input, not a copied constant.
* **F7 — dead and unreachable code.** `Server.allowAccept`
  (`server.go:412–415`) has no production caller (`upgrade` inlines the same
  check); `hostSlot.hostID` (`hub.go:40`) is written, never read;
  `hub.register`'s `unknown_host` branch (`hub.go:107–109`) is unreachable
  because `checkSecret` gates every caller.

**Go 1.26 idioms (the code predates several toolchain gains):**

* **F8 — error codes travel as `fmt.Errorf` strings.** The hub returns
  `fmt.Errorf("limit")`, `fmt.Errorf("host_offline")`, `fmt.Errorf("unknown_session")`,
  and the server switches on `err.Error()` and writes it into wire codes and
  close reasons (`server.go:548–549`, `663`, `786–787`). Idiomatic Go wants
  sentinel errors (`errors.New`) compared with `errors.Is`, with the wire code
  derived from the sentinel — one canonical vocabulary instead of stringly
  control flow.
* **F9 — manual clamp/max patterns predate the `min`/`max` builtins**:
  `server.go:296–303` (sweeper bounds), `653–656` (retry max), `872–878`
  (tick clamp), `config.go:138–146` (`clampInt`), `client.go:132–134`
  (backoff cap).
* **F10 — `wg.Add(2)`+`go` pairs predate `sync.WaitGroup.Go`** (Go 1.25):
  splice `copyDir` (`server.go:933–935`), bridge legs (`client.go:288–336`).
* **F11 — `isNotExist` string-matches `"no such file"`**
  (`fileconfig.go:782–784`) instead of `errors.Is(err, fs.ErrNotExist)`.
* **F12 — `splice_bench_test.go:108` uses `for i := 0; i < b.N; i++`** instead
  of Go 1.24's `for b.Loop()`.
* **F13 — rate keys allocate a string per check** (`bucket + "\x00" + id`,
  `server.go:436`); a comparable struct key `{bucket, id string}` is
  allocation-free on lookup.

**Canonicalization:**

* **F14 — duplicated envelope I/O.** `writeEnv`/`readEnv` exist in both
  `relay` (`server.go:953–977`) and `relayhost` (`client.go:410–431`) with a
  behavioural drift: the server rejects unsupported envelope versions, the
  host client does not. One exported pair in `relay` should serve both.
* **F15 — duplicated flag plumbing.** `bindRelayFlags` binds changed flags
  into viper (`fileconfig.go:494–530`) *and* `newServeCmd` re-applies the same
  flags manually post-`Load` (`cli.go:216–266`) — two mechanisms that must be
  kept in sync by hand. `expandStringList` and `expandDomainList`
  (`fileconfig.go:444–455`, `cli.go:348–359`) are the same function twice.

**Coverage and process:**

* **F16 — coverage sits below the repo's 80% bar**: `internal/relay` 66.4%,
  `internal/relayhost` 71.4%. Cold spots are exactly the failure paths the
  bugs above live in: `handleTunnel` 53.7%, `handleHost` 59.6%,
  `pingHostControl` 43.8%, `ListenAndServe` 56.5%, `runSetupService` 25.5%.
  Neither package is in [0113](./0113-MADR-preexisting-unit-coverage-debt.md)'s
  enumerated floor list, so this debt is currently owned by no record.

**Explicitly not findings** (assessed and accepted): TLS 1.2 floor (first-party
clients all speak 1.3; raising the floor is offered as an option below, not a
defect); plaintext secrets in `MCRELAY_HOSTS` env (documented 0016 trade-off);
`/healthz` minimalism (R11, by design); absent Prometheus metrics (0017 E5,
deliberately deferred, unchanged here); `hub.allow` read without a lock
(immutable after `newHub` — worth a comment, not a mutex).

## Decision Drivers

* The relay is a **public internet edge** in production
  (`wss://headscale.lallygag.net:8443`); pre-auth resource behaviour (F2) and
  self-reclamation of parked connections (F1) matter more here than anywhere
  else in the codebase.
* Prior relay MADRs set a precedent: findings become numbered decisions with
  regression tests, not drive-by fixes.
* The failure paths are the least-tested paths (F16); fixing bugs without
  raising failure-path coverage would repeat the conditions that let F1–F3
  survive three audits.
* Idiom drift (F8–F13) is cheap to fix now, and expensive later once more
  code switches on error strings.
* No protocol or wire-format change is on the table — every fix must be
  invisible to existing phones and hosts.

## Considered Options

* **O1 — one hardening pass: fix F1–F7, adopt idioms F8–F13, canonicalize
  F14–F15, and lift failure-path coverage to the 80% floor**
* **O2 — bugs only (F1–F7), defer idioms/canonicalization/coverage**
* **O3 — record findings, fix nothing now**
* **O4 — O1 plus raising the TLS floor to 1.3**

## Decision Outcome

Chosen option: **"O1 — one hardening pass"**, because the bug fixes and the
idiom/canonicalization work touch the same functions (fixing F1 rewrites the
join hand-off that F8's sentinel errors flow through; F2 touches the same
handlers F16 must cover), so splitting them multiplies review passes over
identical diffs without reducing risk; and because the audit's clearest lesson
is that untested failure paths are where bugs hide — a pass that fixes F1–F3
without tests pinning them is half a fix.

O4 (TLS 1.3 floor) is **deferred, not rejected**: all first-party clients
support 1.3, but the floor change is operator-visible, independently
revertible, and deserves its own smoke window rather than riding a
twelve-finding pass. It can be a one-line follow-up once this pass is verified
in production.

The implementation plan (`0115-PLAN-mcrelay-go126-audit-and-hardening.md`)
will be written and presented separately; **no code changes until that plan is
approved**, per `AGENTS.md`.

### Consequences

* Good, because the two real resource-handling gaps on the public edge (F1,
  F2) close, and the relay stops depending on peer behaviour to reclaim a
  parked tunnel.
* Good, because operator foot-guns (F3, F6) and the rate-limit eviction wobble
  (F4, F5) disappear with zero wire impact.
* Good, because the error vocabulary becomes canonical (F8/F14), making the
  next audit's control-flow reading mechanical instead of forensic.
* Good, because failure-path coverage rises where the bugs actually were, and
  the two relay packages get an owner for their coverage debt (this record,
  not 0113 — 0113's enumerated list is unchanged).
* Neutral, because behaviour changes are confined to failure/edge paths; the
  happy path (register → dial → tunnel → splice) is untouched.
* Bad, because one pass touching ~10 files is a larger review than a bugs-only
  diff (mitigated: the plan will phase it bug-fixes-first so each phase is
  independently revertible).
* Bad, because coverage work on `cli.go`/`runSetupService` (25.5%) has poor
  return on effort — the plan should set the floor per-package, not
  per-function, and say so explicitly.

### Confirmation

* `go vet` and `go test -race -count=1 ./internal/relay/... ./internal/relayhost/...` stay green.
* New regression tests demonstrably fail against the pre-fix tree for F1
  (timeout/claim interleaving: slot released and tunnel unblocked without the
  sweep), F2 (oversized pre-auth first frame refused on phone/tunnel planes),
  F3 (trailing-whitespace `--allow` entry authenticates), F4
  (oldest-window eviction under map pressure), F5 (stalled register exchange
  returns within its deadline).
* `go test -cover` reports ≥ 80% for `internal/relay` and
  `internal/relayhost`.
* Wire compatibility: the existing e2e suite (`e2e_test.go`, 522 lines) passes
  unmodified — no envelope, code, or close-reason visible to current clients
  changes except where a previously stringly code becomes the same string
  emitted from a sentinel.
* A live check against the production relay (`register`, `join`, splice,
  clean shutdown) after deployment, using the existing
  `d4_live_test.go` harness pattern.

## Pros and Cons of the Options

### O1 — one hardening pass (chosen)

* Good, because overlapping diffs are reviewed once.
* Good, because tests land with the code they pin.
* Neutral, because idiom changes (F9–F13) are mechanical and low-risk.
* Bad, because the single PR/pass is the largest of the options.

### O2 — bugs only

* Good, because the smallest possible production diff.
* Bad, because F8/F14 (error vocabulary) would be rewritten *again* when the
  idiom pass lands, touching the same lines twice.
* Bad, because failure-path coverage stays at 53–60% exactly where the bugs
  were found.

### O3 — record only

* Good, because zero risk now.
* Bad, because F1/F2 are live behaviours on a public edge; documenting a
  known pre-auth buffer sizing gap without fixing it is the worst of both.

### O4 — O1 + TLS 1.3 floor

* Good, because it retires TLS 1.2 attack surface on a public listener.
* Bad, because it is the only operator-visible compatibility change in the
  set and would make a twelve-finding pass harder to bisect; better alone.

## More Information

* Audit basis: full read of `cmd/mcrelay` + `internal/relay` +
  `internal/relayhost` at commit `1fd867a` (v0.14.9), `go vet` clean,
  coverage measured with `go test -count=1 -cover`.
* Prior art: [0015](./0015-MADR-mcrelay-transport-security.md),
  [0016](./0016-MADR-mcrelay-audit-hardening.md),
  [0017](./0017-MADR-mcrelay-memory-security-action-plan.md); 0068 P1/P6 and
  0091 D3/D5/D10 for the later edge hardening this audit verified as wired.
* 0017 E5 (metrics) remains deferred and is unchanged by this record.
* Ops context: `docs/ops-mcrelay.md`; production relay
  `wss://headscale.lallygag.net:8443` with one registered host
  (`macos-laptop`).

## Observed — execution results (2026-08-25)

Executed as `0115-PLAN` P1–P8 in eight commits (plus the doc commit), each
phase gated on `make pre-add-check`, `go vet`, and the race-enabled relay and
relayhost suites, with `e2e_test.go` unmodified throughout.

* **Coverage**: `internal/relay` 66.4% → **80.7%** (1176/1457);
  `internal/relayhost` 71.4% → **81.2%** (160/197)
  (`coverage-delta.sh floor --minimum 80.0` → `pass`).
* **Pre-fix failure proofs captured**: F1 via the old-API interleaving
  (publish into an abandoned join succeeded, `phones=1` leaked, `done` never
  closed); F2 via a valid ~65 KiB pre-auth envelope that the old server
  *answered* and the new one kills with `StatusMessageTooBig`; F3 via the
  stored secret `"…abcdef "` retaining the trailing space validation had
  stripped; F5/deviation via watchdog timeouts against the pre-fix tree.
* **Benchmark** (`-benchtime 1x`, single-sample, noise-level):
  1768458 → 2632666 ns/op, 279888 → 229472 B/op, 1204 → 1017 allocs/op.
* **Two owner-approved deviations**, both recorded in the PLAN: the
  template-parity gate required `relay.max_frame_bytes` in three companion
  templates beyond the file list; and writing P6's frame-limit test exposed
  F1's host-side cousin — `bridge`'s TCP leg parked in an uninterruptible
  `tcp.Read` when the WS side died first — fixed with
  `context.AfterFunc(ctx, tcp.Close)` and pinned by
  `TestBridgeUnblocksTCPLegOnWSDeath`.
* **Not shipped**: TLS 1.3 floor (O4, still deferred by decision); 0017 E5
  metrics (unchanged).

Accepted by the owner on 2026-08-25 after reviewing this record. The
production relay has not yet been redeployed; rollout remains a separate
operator action per the PLAN.
