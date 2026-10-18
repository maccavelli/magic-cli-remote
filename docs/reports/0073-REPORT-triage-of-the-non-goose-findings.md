# Triage of 0073's non-Goose findings against today's code

<!-- markdownlint-disable MD013 -->

Observed 2026-10-18, at `master` `56ea5621`. This report decides nothing. It states where each
finding of
[0073-MADR-goose-prompt-hang-and-debug-pass.md](../decisions/0073-MADR-goose-prompt-hang-and-debug-pass.md)
stands today, so that any fix can be planned from facts.

## Why

0073 (2026-08-05) recorded a Goose prompt hang, plus about 30 findings from a parallel code audit of
the transport, session, CLI and Flutter layers. Only F1, F2 and its §10 follow-up were recorded as
fixed. The rest were tracked nowhere.

On 2026-10-18 the owner asked for this triage, while
[0160-MADR-remove-goose-cli-support.md](../decisions/0160-MADR-remove-goose-cli-support.md)'s
amendment of the same date allowed 0073 a "superseded in part" line for its Goose half.

## Method

- **Coverage.** Every finding except F1 and F2. A finding with several parts is split into one row
  per part, giving 46 rows.
- **How the old code was read.** The code 0073 audited is `8719682f^`, read with `git show`. Each
  finding's current equivalent was located with `git blame`, `git log -S`/`-L`, and a grep of
  `docs/` for its ID and for "0073".
- **What counts as FIXED.** A row is FIXED only with a fixing commit and the current code that shows
  the fixed behaviour. A line number that merely moved does not count.
- **How it was done.** Read-only, with no tests run. A helper pass gathered the evidence. These
  claims were spot-checked against the cited lines: F-T1, F-T2's commit, F-S3, §6.2 and §7.1.
- **Line numbers** are at `56ea5621`.

## Summary

| State | Rows | Findings |
| --- | --- | --- |
| FIXED | 7 | F-T2, F-T3 (four of five ops), F-T6, F-T8 (relay half), F-S8(e), §6.1, §6.3 |
| STILL PRESENT | 36 | F3(b), F-T1, F-T4, F-T5, F-T7, F-T8 (daemon half), F-T9, F-S1–F-S7, F-S8(a–d), §6.2, §6.4–§6.7, §6.9, §6.10(a, b, d, e), §6.11 (partial), §7.1–§7.4, §7.5(a–c) |
| OBSOLETE | 2 | §6.8 (Goose config keys), §6.10(c) (`internal/update` deleted) |
| UNCLEAR | 1 | F3(a): 0073's guessed cause did not exist; the table says how to decide it |

**Still present, at High severity in 0073's own ranking:**

- **F-T1:** events for a session whose owner cannot be resolved are dropped with no log.
- **F-S1:** live delivery order can differ from seq order.
- **F-S2:** replayed events are suppressed silently. The trigger is narrowed now that acphttp is gone.
- **F-S3:** two devices claiming the same session can both be told they own it.
- **§6.2:** on macOS, `setup-service --no-start` still stops a running job and never restarts it.
- **§7.1:** the settings screen's own `TranscriptCache` instances bypass the live notifier's write
  serialization.

No later record cites an F-T or F-S ID by name. Several fixed the same defects under their own
numbers: 0091, 0095, 0115, 0137 and 0138. 0090 cites §6.3, §6.4 and §6.8 as its findings 3, 4
and 8.

## What this does not decide

Each still-present finding needs its own MADR and PLAN, or a grouping of them, before any code
changes. Choosing which, and in what order, is the owner's decision.

## Findings

### §3, the Goose hang chain (F3 only)

| ID | Severity (0073) | Summary | State | Evidence | Later record |
| --- | --- | --- | --- | --- | --- |
| F3(a) | S1 | `pair create` token rejected by already-running daemon; hypothesised cause: daemon caches `devices.json` from startup | UNCLEAR (hypothesis refuted) | The hypothesised mechanism did not exist even on 2026-08-05: `Store.Validate` force-reloads the file under the cross-process flock on every call (`internal/auth/store.go:513-528`, blame `e48b208a`, 2026-07-22, which predates 0073). So the observed 401 came from something else. To decide it: reproduce `mcremote pair create` against a running daemon and compare the `data_dir`/`devices.json` path the CLI writes (`mcremote paths`) with the service's (unit/plist env, `--data-dir`). A data-dir split, or the §6.7 TOCTOU, are the remaining candidates. | none |
| F3(b) | S1 | Auth rejections are not logged at warn with a reason | STILL PRESENT | `internal/ws/server.go:3442-3466` `writeAuthError`: `invalid_token`, `client_key_required`, `client_key_mismatch` and `already_authed` write a frame and log nothing. Only the `default` branch logs. The HTTP 401 path `handleHello` (`server.go:516-523`, via `authorizeHTTP` at 490-513) also returns 401 with no log. `handleAuth` (`server.go:1124-1127`) only increments `failedAuths`. | none |

### §4, transport / ws / relay

| ID | Severity (0073) | Summary | State | Evidence | Later record |
| --- | --- | --- | --- | --- | --- |
| F-T1 | High | `BroadcastEvent` silently drops events when `OwnerOf` can't resolve; `Manager.Create` keys by `sess.ID()` not requested id | STILL PRESENT | `internal/ws/server.go:336-343`: `if !ownerKnown { return }` with no log or counter (blame `69188685`, 2026-07-21, unchanged). `internal/session/manager.go:1175,1239-1263` still keys `m.sessions` and `Meta.ID` by `sess.ID()`, with no check that it equals `opts.LocalSessionID`. | none |
| F-T2 | High | Same-id retry of in-flight/failed create/prompt can get no response frame | FIXED | `4d36964d` (2026-08-16) "Introduce retry_no_result". `internal/ws/server.go:982-1013`: the `idemReplay` branch with an empty frame and the `idemWait` branch whose wait yields nothing now write `protocol.ErrRetryNoResult`, or re-begin once and execute. Comment: "Every path here must leave the client with a frame". Pinned by `internal/ws/dispatch_idem_test.go`. | 0095-MADR-post-0094-assessment-and-debug-pass.md F5 / D6 (status table: "F5 Fixed") |
| F-T3 | Med-High | set_mode / set_config_option / cancel / permission.respond / question.respond run inline on the WS read loop with no deadline | FIXED (4 of 5); cancel inline by design | set_mode and set_config are now `dispatchAsync` (`server.go:773-787`, `a59ea111`, MADR 0137 F4). permission.respond and question.respond are async (`server.go:886-894, 903-905`, `80df6ce3`, MADR 0138 F4), with a 15 s op deadline (`server.go:1067-1072`). Residual: `session.cancel` stays inline on purpose (`server.go:788-795`, 0137 F4) and runs on `r.Context()` with no deadline (`server.go:644`, `handleSessionCancel`). A provider `Cancel` that blocks can still stall the read loop. | 0137-MADR-prompt-to-first-token-latency-regression.md F4; 0138-MADR-overhaul-provider-surfaces-and-turn-path.md F4 |
| F-T4 | Medium | Error envelopes are captured as finished results, so retries replay stale `turn_busy`/timeout errors | STILL PRESENT | `internal/ws/server.go:3523-3532`: `writeJSON` calls `s.idem.capture` for every frame with an id, error frames included. `internal/ws/idempotency.go:132-154`: `capture` marks the entry `finished` with that frame. `idempotency.go:166-169`: `fail()` keeps an already-captured frame ("e.g. error envelope; keep it"). A same-id retry within the 10-minute TTL (`idempotency.go:40`) replays the old error. `4d36964d` did not change this. | none |
| F-T5 | Medium | Async-op slots leak when a handler ignores ctx; 8 leaks means permanent `rate_limited` | STILL PRESENT | `internal/ws/server.go:940-970`: the slot is decremented only when the handler goroutine returns. `opCtx` deadlines (`server.go:976-977`) do not reclaim a slot from a handler that ignores ctx. The "async slots exhausted" warn (`server.go:946-951`) predates 0073 (`74eba33c`, 2026-07-27). Since 0138 (`80df6ce3`) the shell lane has its own 2-slot budget (`server.go:184,194`), but the general lane still leaks. | none |
| F-T6 | Medium | Relay claim/publish vs phone-timeout race leaks tunnel and phone slot | FIXED | `bbe9067f` (2026-08-25) "close the join-timeout vs tunnel-claim race (0115 P2, F1)". `internal/relay/hub.go:369-393` `phoneGone` resolves the race under `h.mu`. `publishTunnel` (`hub.go:285-307`) observes `p.phoneGone`, then releases and closes `done`. `abandonTunnel` closes `done` (`hub.go:309-321`). Follow-up double-release fix: `d4ee962c`. | 0115-MADR-mcrelay-go126-audit-and-hardening.md F1 |
| F-T7 | Med-Low | Host-control dial write has no deadline and serializes all joins behind `writeMu` | STILL PRESENT | `internal/relay/hub.go:147-158` `writeControl` takes `slot.writeMu` and calls `WriteEnvelope(ctx, …)` (`internal/relay/protocol.go:106-112`, which sets no deadline). The caller passes the phone's `r.Context()` (`internal/relay/server.go:652, 714`). Only the 10 s control ping (`server.go:640-647`) eventually tears a wedged control connection down. | none |
| F-T8 (relay) | Low | Let's Encrypt TLS config advertises `h2` first; relay should not offer h2 | FIXED | `d2d289ff` (2026-08-15) "harden daemon listeners …". `internal/relay/tls.go:124-133` `stripHTTP2` deletes `h2`/`h2c`, applied at `tls.go:73,106` and `relay/server.go:249,262`. | 0091-MADR-mcrelay-daemon-hardening.md D10 / R43 |
| F-T8 (daemon) | Low | Same `h2`-first ALPN on the mcremote daemon's letsencrypt path | STILL PRESENT | `internal/certs/acme.go:177` still prepends `"h2","http/1.1"`. The daemon uses it unstripped: `internal/daemon/certs.go:107` → `daemon.go:529` (`httpServer.TLSConfig = identity.Config`) → `daemon.go:617` `ServeTLS`, which enables HTTP/2. 0091 D10 scoped the fix to mcrelay only. Low impact: the self-signed default is unaffected and the phone's dart:io WS does not offer h2. | none |
| F-T9 | Low | Bearer-header auth on upgrade sends no `auth_ok` and never negotiates v2 | STILL PRESENT | `internal/ws/server.go:645-650`: on success it calls `s.authenticate(c, token)` and writes nothing. `c.negotiated` stays 0, so the connection is pinned to v1 (`handleMessage` maxV, `server.go:717-720`). Blame `d99a9358`/`7daf98b8` (2026-07-19/20), unchanged. | none |

### §5, session / event layer

| ID | Severity (0073) | Summary | State | Evidence | Later record |
| --- | --- | --- | --- | --- | --- |
| F-S1 | High | Seq stamped under `m.mu`, broadcast after unlock; `advertiseCommands` broadcasts seq N+1 before triggering seq N | STILL PRESENT | Seq is stamped in `appendHistoryLocked` (`internal/session/manager.go:315`), called at `manager.go:1496` under the lock. After unlock the pump calls `m.advertiseCommands` (`manager.go:1546-1549`), context-pressure notices (1550-1557) and budget notices (1558-1568) first. Each goes through `emitEvent` (`internal/session/commands.go:1561-1576`: stamp, unlock, broadcast) before the triggering event's own `m.onEvent(ev)` at `manager.go:1586`. The deterministic inversion remains. | none |
| F-S2 | High | `Replay=true` events suppressed from live broadcast with no diagnostic; a stuck acphttp `loading` flag turns a live session into history-only | STILL PRESENT (narrowed) | Suppression is still silent: `internal/session/manager.go:1582-1588` (`if mine && !ev.Replay && m.onEvent != nil`, no log or counter). The specific trigger (acphttp `loading`) is gone with acphttp (`9a567bf1`, MADR 0160). The remaining stamper, `acpagent`, clears its flag unconditionally after `LoadSession` returns, bounded by `loadTimeout` (`internal/provider/acpagent/acpagent.go:885-898`). `httpagent.EmitReplay` (`internal/provider/httpagent/session.go:1903-1910`) is pre-attach only. | 0160-MADR-remove-goose-cli-support.md (trigger removal only) |
| F-S3 | High | `Authorize` first-touch claim race: two devices both told "authorized", memory and disk owners diverge | STILL PRESENT | `internal/session/manager.go:1722-1745`: it checks owner `""`, unlocks, `persistNow(claimMeta)`, relocks, and stamps only if still `""`, but returns `nil` in both cases. Two concurrent claimers both persist (last write wins on disk) and both get `nil`, while memory keeps the first. There is no `lockCreate` in `Authorize`. Blame `83f26386` (2026-07-30), unchanged. | none |
| F-S4 | Med-High | Seq reuse within one epoch after a failed close-time `SaveHistory` | STILL PRESENT | Close path: `internal/session/manager.go:3198-3203` only logs a `SaveHistory` failure. Re-create seeds `seq` from durable history (`manager.go:1218-1226, 1252-1258`), so a failed save lowers `priorSeq` below seqs already sent. The epoch is re-minted only after an unclean shutdown (`manager.go:878-892`), not on a save failure. | none |
| F-S5 | Medium | `FlushHistory` vs `Close`/`Delete` race: stale snapshot clobbers final transcript or resurrects a purged dir | STILL PRESENT | `internal/session/manager.go:3497-3547`: it snapshots under `RLock` and then calls `m.store.SaveHistory` outside any per-session serialization, with no `isPurged` check (unlike `writePersist`, `manager.go:3405, 3445`). `Store.SaveHistory` does `MkdirAll` (`internal/session/store.go:288-307`), so a write after `Delete` recreates the dir. `git log -L` shows no change to `FlushHistory` since 0073. The `Degraded` consequence has changed shape (List now warns and skips, `store.go:~189-205`). | none |
| F-S6 | Medium | `lockCreate` not ctx-aware and held across provider `Start` | STILL PRESENT | `internal/session/manager.go:828-848`: a plain `sync.Mutex`. It is taken at `manager.go:1053` and held across `p.Start(ctx, opts)` at `manager.go:1150`. A waiter cannot abandon on ctx. | none |
| F-S7 | Medium | `/clear` doesn't clear the durable ring; cold reload replays the pre-clear conversation | STILL PRESENT | `/clear` → `cmdReset` → `relaunch` → `Create(LocalSessionID: id)` (`internal/session/commands.go:296-298, 1486-1506, 1547-1556`). `Create` closes the old process, which saves history (`manager.go:3198`), then re-seeds the ring from `LoadHistory(sess.ID())` (`manager.go:1221`). The `relaunch` doc comment ("history ring is reset") is false. | none |
| F-S8(a) | Low-Med | Pump does fsync'd multi-session history flushes before broadcasting | STILL PRESENT | `manager.go:1571` `scheduleHistoryPersist` runs before `onEvent` (1586). When an id has been dirty for at least `historyMaxLatency` (5 s, `manager.go:820`), it calls `FlushHistory()` synchronously (`manager.go:3472-3477`), writing every dirty session with `SyncFile/SyncDir` (`store.go:88-89`). | none |
| F-S8(b) | Low-Med | Notices for untracked sessions broadcast with `Seq=0` | STILL PRESENT | `internal/session/commands.go:1561-1576` `emitEvent`: when `!mine` there is no `appendHistoryLocked`, so `Seq` stays 0, but `m.onEvent(ev)` is still called. | none |
| F-S8(c) | Low-Med | Cleared pending asks (turn end) emit no `permission_resolved` | STILL PRESENT (server); impact mitigated | `internal/session/manager.go:1519-1521` still does `clear(e.pendingPermissions/Questions)` without emitting resolved events. Mitigations: acpagent sweeps outstanding permissions with resolved events (`internal/provider/acpagent/session.go:914, 930-…`), codex `sweepPendingApprovals` (`codex/session.go:609`), and the phone clears all pending on `turn_complete` (`apps/mobile/lib/data/chat/transcript_reducer.dart:435-438`). The phone mitigation predates 0073 (`cd27bc61`, 2026-07-20), so the stale-card impact was overstated. | none |
| F-S8(d) | Low-Med | `agenterr` quota keywords over-broad | STILL PRESENT | `internal/agenterr/agenterr.go:72-98` still has the broad substrings (`"limit reached"`, `"usage limit"`, `"free usage"`), and `hardQuotaWords` (`agenterr.go:215-230`) still has bare `"credit"`, `"plan"`, `"spend"`, `"quota"`, now widened with `"weekly"` and `"monthly"`. All are matched as raw substrings (`containsAny`, `agenterr.go:238-245`). | none |
| F-S8(e) | Low-Med | `HistoryPage` O(n²) marshal | FIXED | `2893c3f9` (2026-09-03, MADR 0138). `internal/session/manager.go:1935-1954` now uses `historyBudgetPrefix` (`manager.go:2091-2097…`), which encodes each event once. The old loop re-marshalled the shrinking page (old `manager.go:893-905`). | 0138-MADR-overhaul-provider-surfaces-and-turn-path.md F14 |

### §6, CLI / config / auth / update

| ID | Severity (0073) | Summary | State | Evidence | Later record |
| --- | --- | --- | --- | --- | --- |
| §6.1 | High | `HealStart` unconditionally true: update on a host with no service swaps, fails to start, rolls back | FIXED | `internal/update/*` was deleted by `45f82032` (2026-09-02, adopt mcplib selfupdate). `internal/updateclient/lifecycle.go:39-52` `Installed` distinguishes "no service definition". mcplib v1.4.1 `selfupdate/managed.go:66-71`: when not installed it does a standalone install with no Start and no rollback. The `Lifecycle.Installed` doc comment names this exact failure. | (MADR 0005 per code comments; no record cites 0073 §6.1) |
| §6.2 | High | macOS `setup-service --no-start` still boots out a running service and never bootstraps it | STILL PRESENT | `internal/cli/service/setup.go:505`: `stopAndWaitDarwin(svc)` runs unconditionally (teardown since `46190650`, 2026-08-29). Bootstrap and kickstart are gated on `!opts.NoStart` (`setup.go:516-531`). The kickstart-only fast path also requires `!opts.NoStart` (`setup.go:489`). The flag help says "do not start/restart the service" (`internal/cli/setup_service.go:86`). | none |
| §6.3 | High | Provisioned configs write `permission_mode: ""` for grok; parity test never compares | FIXED | `e98845f1` (2026-08-15; commit body cites MADR 0090). `internal/cli/service/defaults_mcremote.yaml:56` and `configs/config.example.yaml:94` now say `"default"`, pinned by `TestTemplateGrokPermissionModeIsDefault` (`internal/cli/service/template_parity_test.go:256`). Note: 0090's front matter still says `status: proposed`. | 0090-MADR-config-template-completeness.md D4; 0090-PLAN-config-template-completeness.md |
| §6.4 | Medium | Missing `SetDefault`s, so env vars for `limits.ws_read_deadline_seconds`, `ws_resume_window_seconds`, `tcp_keepalive.*`, `providers.codex.sandbox_broken_policy` are ignored | STILL PRESENT | `internal/config/load.go:287-389` `setDefaults` has only `limits.max_ws_clients`/`max_live_sessions` (374-375) and no `sandbox_broken_policy`, while the keys exist (`internal/config/config.go:136,140,144,646`). `0090-PLAN` Phase 3 Task 3.1 ("`setDefaults` … 0073 finding 4") is still unchecked. | 0090-MADR (D6) / 0090-PLAN Phase 3, planned, not done |
| §6.5 | Medium | Raw `--data-dir` re-applied over Load's absolutized value; relative paths hard-error | STILL PRESENT | `internal/cli/serve.go:54-58` and `internal/cli/paths.go:30-35` assign the raw flag after `config.Load`, which already absolutized it (`internal/config/load.go:188,196`). They then call `RecomputePaths`, and `appdirs.WithDataDir` rejects non-absolute paths (`internal/appdirs/paths.go:108-110`). | none |
| §6.6 | Medium | Update swaps the running binary's path, not the service ExecStart binary; reports success while the daemon runs the old build | STILL PRESENT | The target is still `os.Executable()` (mcplib v1.4.1 `selfupdate/target.go:12-18,71-75`). Refresh preserves the unit's own binary (`internal/cli/service/refresh.go:88-95, 421, 683`). `binaryWarning` (`refresh.go:550-568`, `fe3a0a9f`, MADR 0100 Phase 1) detects the mismatch, but `mcremote update` never surfaces it: the reconcile `Detail` is dropped by mcplib (`managed.go:87-90`; `updater.go:244-252` reports a fixed detail). `WaitHealthy` checks "active", not version (`internal/updateclient/lifecycle.go:94-133`). | 0100-PLAN-update-unit-refresh-and-daemon-reload.md (warning only) |
| §6.7 | Medium | `OpenStore` creates/truncates `devices.json` outside the flock (TOCTOU with CLI `pair create`) | STILL PRESENT | `internal/auth/store.go:224-236`: `os.Stat`, then `persistLocked(empty)` with no `withPathLock`. `persistLocked` (`store.go:579-597`) takes no path lock. Blame `7daf98b8`/`55c5a7ec` (July), unchanged. | none |
| §6.8 | Medium | `providers.goose.args` / `fs_roots` parse but are silently discarded | OBSOLETE | The Goose provider was removed by `9a567bf1` (2026-09-18). Any `providers.goose` block or `MCREMOTE_PROVIDERS_GOOSE_*` var now produces a warn and a `retired_provider_goose` diagnostic (`internal/config/retired_goose.go:27-49`). | 0160-MADR-remove-goose-cli-support.md D3 |
| §6.9 | Medium | systemd unit `PrivateTmp=true` vs appdirs runtime fallback under `os.TempDir()` can split the admin socket | STILL PRESENT | `internal/cli/service/mcremote.user.service.tmpl:62` `PrivateTmp=true`. `internal/appdirs/roots_unix.go:84-91` still falls back to `os.TempDir()` when neither `XDG_RUNTIME_DIR` nor `/run/user/$UID` exists. The template's optional `Environment=XDG_RUNTIME_DIR` (`tmpl:~50`) existed in 0073's tree too. Latent, as before. | none |
| §6.10(a) | Low | `picker/cache.go` inflight entry leaks on fetch panic | STILL PRESENT | `internal/picker/cache.go:98-117`: `fetch(ctx)` has no `defer`. A panic skips `delete(c.inflight, key)` and `close(cl.done)`. Later callers block until their ctx ends. `dispatchAsync` recovers panics (`internal/ws/server.go:961-965`), so the daemon survives into the leaked state. | none |
| §6.10(b) | Low | `paircode.go` unchecked burns | STILL PRESENT | `internal/auth/paircode.go:189, 196`: `_ = s.save(data)` (identical to old lines 189/197). | none |
| §6.10(c) | Low | `update/github.go` SHA256SUMS prefix match | OBSOLETE | `internal/update/github.go` deleted by `45f82032`. Replacement mcplib v1.4.1 `selfupdate/assets.go:8,88-103` accepts only the exact asset name `SHA256SUMS`. | none |
| §6.10(d) | Low | `service/control.go` hard-coded unit label vs `--unit-name` | STILL PRESENT | `internal/cli/service/control.go:236-238` `launchdLabel(product)`, used at 19, 87, 138, 188. systemd uses `product + ".service"` at 40, 66, 111, 142, 208. Meanwhile `setup.go:166-191` honours `--unit-name`. | none |
| §6.10(e) | Low | smoke-protocol v1-only with dead `if waitAfter > 0 {}` | STILL PRESENT | `scripts/smoke-protocol/main.go:404-406` (empty block). The auth payload offers no `protocols` (`main.go:128`), so the run negotiates v1. | none |
| §6.11 | (unranked, "also noted") | No "claude" engine; `doctor` has zero per-engine checks (binary presence, readiness) | STILL PRESENT (partial) | `internal/cli/doctor.go:18-36` now also renders per-agent credential-file presence (`9f5a6677`, 2026-08-10, MADR 0074 §9) and a codex credential-store probe (`beba2d14`). There is still no per-engine binary-presence or provider-readiness/hung-engine check. There is still no claude provider (`internal/provider/` has acpagent, codex, grok, httpagent, kilo, opencode, fake), which is informational. | 0074-MADR-remote-provider-auth-from-phone.md §9 (credentials part only) |

### §7, Flutter app

| ID | Severity (0073) | Summary | State | Evidence | Later record |
| --- | --- | --- | --- | --- | --- |
| §7.1 | High | `TranscriptCache` serialization defeated by second instances in settings | STILL PRESENT | `apps/mobile/lib/features/settings/settings_screen.dart:591` (`usage`) and `:628` (`clear`) build `TranscriptCache()` afresh. `_serial` is an instance field (`apps/mobile/lib/data/chat/transcript_cache.dart:162-171`), so they don't serialize against the notifier's instance (`lib/state/transcripts_notifier.dart:129`). | none |
| §7.2 | Medium | Hand-editing Host clears pair hints without `setState` | STILL PRESENT | `apps/mobile/lib/features/connect/connect_screen.dart:133-136` calls `_clearPendingPairHints()` (`:436-444`, plain field writes) with no `setState`. The UI rebuilds only when the 600 ms-debounced probe refresh completes (`:140-144`). That was equally true in 0073's tree. | none |
| §7.3 | Medium | `_busy` released between clipboard read and claim; double-tap can burn a one-shot pair code | STILL PRESENT | `connect_screen.dart:865-877`: `_busy` is cleared in `finally` right after `Clipboard.getData`. Then `_applyPair` (`:758-…`) awaits `_refreshProbes()` without re-setting `_busy`, and `_claimCode` is reached at `:900`. Code is unchanged since 0073 (only `c1f0beb0` touched the file). | none |
| §7.4 | Medium | Post-frame streaming callback doesn't re-check `widget.streaming` | STILL PRESENT | `apps/mobile/lib/features/chat/chat_bubble.dart:1031-1040`: the callback checks `mounted` and `_upToDate(true)` only, then `_updateStreamingRender()` (`:1043-1049`) renders with `streaming=true`. The tier-timer path does check `widget.streaming` (`:1019`). The post-frame path does not. | none |
| §7.5(a) | Low | `settings_screen` unguarded `clearSecrets()` (silent dead tap on keystore failure) | STILL PRESENT | `settings_screen.dart:566` `await store.clearSecrets();` has no try/catch. `clearSecrets` rethrows `SecureStorageUnavailable` (`apps/mobile/lib/data/local/settings_store.dart:417-431`). | none |
| §7.5(b) | Low | `notification_service.init` not re-entrancy guarded | STILL PRESENT | `apps/mobile/lib/data/notifications/notification_service.dart:118-119`: only `if (_ready) return;`, and `_ready` is set after awaits. There is no in-flight future. `_ensureReady` (`:109-116`, MADR 0129) adds more concurrent callers. | none |
| §7.5(c) | Low | `app_update.dart` catch can mask the original error and leak a partial APK | STILL PRESENT | `apps/mobile/lib/data/update/app_update.dart:151-155`: `await sink.close()` inside `catch` can throw before `dest.delete()`, masking `e` and leaving the file. Same shape as the old lines 115-120. Later commits `05163781` and `dd29f5f5` did not touch this block. | none |
