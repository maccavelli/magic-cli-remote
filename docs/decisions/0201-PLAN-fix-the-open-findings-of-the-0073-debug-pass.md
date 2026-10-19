---
status: in-progress
date: 2026-10-18
associated-madr: "0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement fixing every open finding from 0073's debug pass

Associated MADR: [0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md](0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md)

- **Approval.** The owner accepts the MADR and approves each phase by name ("do P1").
  - P0–P8 do not depend on
    [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md).
  - P9 waits for 0054's acceptance and its S1 and S3.
  - P10 waits for the phases it verifies.
- **The owner commits each phase** (`git commit --no-edit`). An agent stages only.

## Goal

Every finding in
[0073-REPORT-triage-of-the-non-goose-findings.md](../reports/0073-REPORT-triage-of-the-non-goose-findings.md)
marked STILL PRESENT or UNCLEAR, plus the F-T3 residual, is fixed as the MADR's D1–D39 decide.
Each fix:

- has a test that was seen failing on the code before it;
- holds through the planted breaks listed for its phase;
- passes the phase gates.

The triage report then marks each finding FIXED, naming its commit.

## Scope

| Phase | Decisions | Files (production; tests beside them) |
| --- | --- | --- |
| P0 | D2 step 1 | none (baseline and reproduction only) |
| P1 | D1, D3, D5, D6, D9, D10 | `internal/ws/server.go`, `internal/ws/idempotency.go`, `docs/guides/protocol-v1.md` |
| P2 | D7, D8 | `internal/relay/hub.go`, `internal/daemon/daemon.go`, `internal/certs/acme.go` |
| P3 | D4, D11, D12, D13 | `internal/session/manager.go`, `internal/session/commands.go` |
| P4 | D14, D15, D16, D17, D18, D19, D20 | `internal/session/manager.go`, `internal/session/commands.go`, `internal/session/store.go` |
| P5 | D21 | `internal/agenterr/agenterr.go` |
| P6 | D2, D22, D23, D24, D25, D27, D28, D29, D31, D32 | `internal/cli/pair.go`, `internal/cli/serve.go`, `internal/cli/paths.go`, `internal/cli/doctor.go`, `internal/cli/service/setup.go`, `internal/cli/service/setup_schtasks.go`, both `*.user.service.tmpl`, `internal/config/load.go`, `internal/config/config.go`, `internal/auth/store.go`, `internal/auth/paircode.go`, `internal/picker/cache.go`, `scripts/smoke-protocol/main.go` |
| P7 | D33–D39 | `apps/mobile/lib/data/chat/transcript_cache.dart`, `apps/mobile/lib/features/connect/connect_screen.dart`, `apps/mobile/lib/features/chat/chat_bubble.dart`, `apps/mobile/lib/features/settings/settings_screen.dart`, `apps/mobile/lib/data/notifications/notification_service.dart`, `apps/mobile/lib/data/update/app_update.dart` |
| P8 | records | the triage report, `0090-PLAN`, protocol guides |
| P9 | D26, D30, the §6.1 check | the update and service code as 0054 S1/S3 leave it |
| P10 | real-host checks | none (verification only) |

**Out of scope:**

- the seven findings already fixed, and the two obsolete;
- §6.11's "no claude engine" remark;
- any `mcplib` change, since 0054 removes it.

## Rules for every phase

1. **Name the tree.** Start from a clean `master` and record `git rev-parse --short HEAD` in the
   phase's execution record.
2. **Test first.** For each decision:
   - write the named test first;
   - run it, and copy the failing assertion into the execution record (a failure to compile does
     not count: give the type or function a stub first);
   - then implement.
3. **Planted breaks** are run on a scratch copy made by `rsync -a --exclude .git --exclude apps`
   (with `apps` included for P7), by a scratchpad Python script that asserts each anchor occurs
   once. A plant that fails to compile is re-planted until it fails an assertion.
4. **Gates.** In order, each phase runs:
   - `make pre-add-check FILES="<the phase's Go files>"`;
   - `go vet ./...`;
   - `go test -race -count=1 ./...`;
   - `make lint`;
   - `make staticcheck`;
   - `make ci-windows` on the Windows test host, through the scratch harness;
   - `python3 scripts/check_records.py --check-all`.

   P7 also runs `dart format` on its files and `make preflight`.
5. **Stage, then record.** Stage only the phase's files. Write its execution record (the failing
   outputs, the plants, the gate results), and mark its decisions done in this PLAN.
6. **Deviations stop the phase.** Raise any deviation with evidence and options. Record the
   owner's choice as a dated entry here, and in the MADR when a decision changes.

## Implementation Steps

### P0 — Baseline, and F3(a)'s reproduction (D2 step 1)

1. `go test -race -count=1 ./...` at the starting commit. Record the result. A failure that
   predates this PLAN is recorded, not fixed here.
2. **Reproduce F3(a)**, on the Mac, in the session scratch directory:
   - Run a scratch daemon with data directory A (`XDG_*` all in the scratch directory, as 0187
     T4's run did, `listen 127.0.0.1:<port>`).
   - Run `mcremote pair create --data-dir B` with a different directory B, then claim the code
     against the daemon with a scratch client (`scripts/smoke-protocol` or a Go test client).
   - Record the outcome. A rejection with `invalid_token` confirms the data-directory split.
   - Repeat with B = A. The claim must succeed, which shows the split, not the store, is the
     cause.
   - Tear down: the daemon is stopped, and the scratch data and runtime directories are removed.

### P1 — Transport and authentication (D1, D3, D5, D6, D9, D10)

All tests are in `internal/ws`, using the existing `httptest` server helpers in `server_test.go`.

- **D1.** New file `internal/ws/authlog_test.go`:
  - `TestEveryAuthRejectionIsLoggedWithoutTheToken`: for each of `invalid_token`,
    `client_key_required`, `client_key_mismatch`, `already_authed`, and a 401 on `/v1/hello`,
    exactly one `warn` record carries `code` and `remote_addr`, and no record contains the token
    string.
  - `TestAuthRejectionsFromOneAddressAreThrottled`: 20 rejections from one address within a
    minute produce one record. After the window, one more record carries `suppressed=19`.
  - The clock is injected through a `now func() time.Time` field on `Server`, which defaults to
    `time.Now`.
  - **Implement:** a small `authLogLimiter` (a map of `code|addr` to window start and count,
    bounded at 1024 entries, oldest first out), used by `writeAuthError` and by the `/v1/hello`
    401 path.
- **D3.** `TestEventsForUnownedSessionsAreLoggedOncePerSession` (`internal/ws/broadcast_test.go`):
  - two events for one unknown id log one `info` record, and a second unknown id logs another;
  - a purged id logs at `debug`, through `OwnerOf` returning the purged state.
  - **Implement:** `session.Manager` gains `OwnerState(id) (owner string, state OwnerState)`,
    with states known, purged and unknown, read from `m.sessions`, `m.isPurged` and the store.
    The `ws` server holds the concrete `*session.Manager` (`server.go`, field `sessions`).
    `BroadcastEvent` calls `OwnerState` instead of `OwnerOf`, and logs through a bounded set of
    256 ids. `OwnerOf` stays for its other callers.
- **D5.** `TestErrorFramesAreNotReplayedToARetry` (`internal/ws/dispatch_idem_test.go`):
  - a mutating request whose handler writes a `turn_busy` error, retried with the same id,
    reaches the handler twice;
  - `TestSuccessFramesStillReplay`: a successful request is answered from the ledger, and the
    handler runs once.
  - **Implement:** `writeJSON` calls `s.idem.fail(...)` instead of `capture` when
    `env.Type == protocol.TypeError`.
- **D6.** `TestAnOverrunningHandlerReturnsItsSlotAtTheDeadline`:
  - the operation timeout is overridden to 50 ms and the grace to 20 ms. `asyncOpTimeout` is a
    function today, so `dispatchAsync` calls it through a package variable,
    `asyncOpTimeoutFor = asyncOpTimeout`, that tests replace. The grace is a package variable,
    `asyncSlotGrace` (default 5 s);
  - eight handlers block, ignoring their context;
  - after 70 ms a ninth operation is accepted, not `rate_limited`;
  - `TestASlotIsReturnedOnce`: when the handler returns after the deadline, the in-flight count
    stays at 0 and never goes negative.
  - **Implement:** the slot release goes into a `sync.Once`, called from the handler's `defer`
    and from a `time.AfterFunc(timeout+grace)`. An overrun logs `warn` with the type and the
    elapsed time.
- **D9.** `TestBearerUpgradeReceivesAuthOK`: a WebSocket dial with `Authorization: Bearer
  <token>` receives a first frame of type `auth_ok` with no `protocol` field, and `session.list`
  then succeeds. `docs/guides/protocol-v1.md`, at "Optional: `Authorization: Bearer`", gains one
  sentence: the server answers `auth_ok` for protocol 1, and a client wanting v2 sends `auth`.
- **D10.** `TestABlockedCancelDoesNotHoldTheReadLoop`:
  - a real `session.Manager`, which the server holds concretely, over a test provider whose
    session `Cancel` blocks until the test releases it;
  - the cancel bound is overridden to 100 ms (package variable `cancelBound`, default 10 s);
  - a `session.cancel` gets a `session_cancel_timeout` error within 200 ms, and a following
    `ping` is answered.
  - **Implement:** `handleSessionCancel` runs `s.sessions.Cancel` in a goroutine and selects on
    its result or `time.After(cancelBound)`. `session_cancel_timeout` is added to the error codes
    in `internal/protocol` and in the protocol guide's error table.
- **Planted breaks:**
  - D1: the limiter key without `addr`;
  - D5: `fail` replaced by `capture`;
  - D6: the `AfterFunc` removed;
  - D9: the `auth_ok` write removed;
  - D10: a plain blocking call.

  Each must fail its test.

### P2 — Relay and daemon TLS (D7, D8)

- **D7.** `TestWriteControlHasADeadline` (`internal/relay/hub_test.go`):
  - a control connection whose peer never reads, with the bound overridden to 100 ms (package
    variable `controlWriteTimeout`, default 10 s);
  - `writeControl` returns within 300 ms;
  - a concurrent second `writeControl` for the same host also returns within the bound.
  - **Implement:** `writeControl` derives `context.WithTimeout(ctx, controlWriteTimeout)`.
- **D8.** `TestDaemonNegotiatesHTTP1Only` (`internal/daemon/tls_alpn_test.go`):
  - starts the daemon's HTTP server, as `daemon.go` builds it, in self-signed mode on
    `127.0.0.1:0`;
  - dials with `tls.Config{NextProtos: []string{"h2", "http/1.1"}, InsecureSkipVerify: true}`;
  - requires `ConnectionState().NegotiatedProtocol == "http/1.1"`;
  - seen failing first (it negotiates `h2` today, as the MADR measured).

  `TestACMEConfigOffersNoH2` (`internal/certs/acme_test.go`) requires that
  `ACMEBundle.TLSConfig().NextProtos` contains no `h2`, through a constructor that does not
  contact an ACME server.
  - **Implement:**
    - `httpServer.Protocols = new(http.Protocols)` with `SetHTTP1(true)`, in `daemon.go` where
      `httpServer` is built;
    - `acme.go` uses `append([]string{"http/1.1"}, cfg.NextProtos...)`, with any `h2` removed.
- **Planted breaks:** D7, the timeout removed; D8, `Protocols` left nil. Each must fail its test.

### P3 — Session ordering and ownership (D4, D11, D12, D13)

All tests are in `internal/session`, using `internal/provider/fake` and the helpers in the
`manager_*_test.go` files.

- **D4.** `TestCreateRefusesAMismatchedSessionID`: a test provider whose session reports
  `"other"` when asked for `"want"` makes `Create` return `ErrSessionIDMismatch`. The provider's
  `Close` is called, and no entry exists under either id.
- **D11.**
  - `TestFirstEventsOfASessionArriveInSeqOrder`: the `onEvent` hook records the seqs of the first
    20 events of a new fake session, including the `advertiseCommands` they trigger. They must be
    strictly increasing.
  - `TestConcurrentEmitsKeepSeqOrder`: 4 goroutines calling `emitEvent` while the pump delivers
    200 provider events, for one session. The recorded seqs must be strictly increasing. The test
    runs under `-race`.
  - **Implement:**
    - `entry.deliverMu sync.Mutex`;
    - in the pump: take `e.deliverMu`, then `m.mu`, stamp, unlock `m.mu`, call `onEvent(ev)` for
      the triggering event, then the derived events (`advertiseCommands`, the notices), each
      through `emitEventLocked`, which assumes `deliverMu` is held, then release `deliverMu`;
    - `emitEvent` takes `deliverMu` the same way;
    - the lock order is written in a comment on `entry`.
- **D12.** `TestReplaySuppressionIsCountedAndLogged`:
  - 5 replay events and then one live event produce one `debug` record, with `count=5`;
  - `TestAReplayEventDuringATurnWarns`: with a turn in flight, `promptAt` set, one replay event
    produces one `warn` record, and a second in the same turn produces none.
- **D13.** `TestOnlyOneConcurrentClaimWins`:
  - 200 rounds; in each, two goroutines call `Authorize(id, devA|devB, true)` on an unowned
    session;
  - exactly one gets `nil`, the other `ErrForbidden`, and `store.Get(id).OwnerDeviceID` equals
    the in-memory owner.
  - **Implement:** `entry.claimMu`, taken before `m.mu` in `Authorize`'s claim path. The check
    and the stamp happen under it.
- **Planted breaks:**
  - D11: the triggering `onEvent` moved back after the notices;
  - D13: `claimMu` removed;
  - D4: the equality check removed.

  Each must fail its test.

### P4 — Session persistence (D14–D20)

- **D14.** `TestSeqStaysAheadAfterAFailedCloseSave`: a store whose `SaveHistory` fails on close.
  After `Close` and a re-`Create`, the first event's seq is greater than every seq delivered
  before. `Meta`/`Record` gain `LastSeq uint64` (`json:"last_seq,omitempty"`), set in the close
  path and read in `Create`'s seeding: `priorSeq = max(lastHistSeq, rec.LastSeq)`.
- **D15.**
  - `TestStaleFlushCannotOverwriteTheCloseTranscript`: a test hook pauses `FlushHistory` after
    its snapshot; `Close` writes the final transcript; the flush resumes. The file on disk must
    hold the close-time transcript.
  - `TestFlushAfterDeleteLeavesNoDirectory`: the flush resumes after `Delete`, and the session
    directory does not exist.
  - **Implement:**
    - `entry.histGen uint64`, increased in `appendHistoryLocked` and in any ring trim;
    - `m.historyWriteMu`, plus `m.savedHistGen map[string]uint64`;
    - `m.saveHistory(id, snap, gen)`: lock, skip when `isPurged(id)` or `gen <= savedHistGen[id]`,
      `store.SaveHistory`, record `gen`;
    - flush, close and reset all call it, and `Delete` removes history under `historyWriteMu`.
- **D16.** `TestCreateLockWaitEndsWithTheContext`: a provider whose `Start` blocks until released.
  A second `Create` for the same id, with a 100 ms context, returns `context.DeadlineExceeded`
  within 300 ms. `createLock.mu` becomes `ch chan struct{}` (cap 1), and
  `lockCreate(ctx, id) (func(), error)`. Its callers propagate the error.
- **D17.** `TestClearResetsTheDurableTranscript`:
  - after three turns, `/clear`, then one turn, `HistoryPage` from a cold store returns only the
    reset notice and the new turn, and their seqs exceed the last pre-clear seq;
  - `TestModelSwitchKeepsTheTranscript` pins the other relaunch.
  - **Implement:** `createOptions{resetHistory bool}`, internal; `relaunch(…, reset bool)`;
    `cmdReset` passes `true`; the doc comment is corrected.
- **D18.** `TestPumpNeverWaitsOnAHistoryFlush`: a store whose `SaveHistory` blocks for 2 s, with
  the latency bound overridden to 10 ms (package variable). 50 events stream, and each `onEvent`
  arrives within 50 ms of its provider emit. `scheduleHistoryPersist` replaces the inline
  `FlushHistory()` with `m.historyTimer.Reset(0)`, or `AfterFunc(0, …)` when it is nil.
- **D19.** `TestNoticesForClosedSessionsAreSequencedAndDurable`:
  - a session closed with `LastSeq=N`, then `emitNotice(id, "x")`;
  - the broadcast event has `Seq == N+1`, and a cold `HistoryPage` includes it;
  - `TestNoticesForUnknownSessionsAreNotBroadcast`: no `onEvent` call.
- **D20.** `TestAsksOpenAtTurnEndAreResolvedCancelled`:
  - one permission and one question pending at `turn_complete` produce one `permission_resolved`
    and one `question_resolved`, each `cancelled`, sequenced after the `turn_complete`;
  - one already resolved during the turn produces none extra.
- **Planted breaks:**
  - D14: seeding from history only;
  - D15: the generation check removed;
  - D16: the `select` on `ctx` removed;
  - D17: `resetHistory` ignored;
  - D18: the inline flush restored;
  - D20: no synthetic events.

  Each must fail its test.

### P5 — Quota classification (D21)

1. `TestQuotaWordsMatchWholeWords` (`internal/agenterr/agenterr_test.go`), as a table that must
   classify as not quota:
   - "the explanation of rate limit 429";
   - "account suspended: rate limit";
   - "accredited endpoint: rate limit";
   - "context length limit reached";
   - "maximum token limit reached";
   - "planned maintenance: too many requests".

   It must pass unchanged on every message the existing corpus tests already hold.
2. **Implement:**
   - `hardQuotaWords`' single words compile once into one `\b(billing|credits?|quota|spend|spending|plan|plans|weekly|monthly)\b` expression, and the multi-word entries stay as substrings;
   - `limit reached` is skipped when `context|token|length` appears within the same sentence.
3. **Planted break:** the old substring list restored, which must fail the table.

### P6 — CLI, configuration, store and installer (D2, D22–D25, D27–D29, D31, D32)

- **D2.** `TestPairCodeWarnsWhenNoDaemonServesItsDataDir` (`internal/cli/pair_test.go`):
  - a TCP listener on the configured port, and no admin socket: stderr contains
    `no daemon serves this data directory`;
  - with a fake admin socket answering `ping`: no warning.
  - **Implement:** after writing the code, `pair`, `pair code` and `pair create` call a new
    `warnIfDaemonMissesDataDir(cfg)`.
- **D22.** `TestNoStartLeavesARunningLaunchdJobAlone` and `TestNoStartDoesNotStartTheWindowsTask`
  (`internal/cli/service`, using the existing fake runners): under `NoStart`, the recorded
  launchctl calls contain no `bootout` and no `bootstrap`, the schtasks calls contain no `/run`,
  and the definition file is written. In `setupLaunchdAgent`, `stopAndWaitDarwin` and the
  bootstrap move under `!opts.NoStart`. In `setupSchtasks`, `startWindows` moves under
  `!opts.NoStart`. When a running job exists under `NoStart`, one line goes to stderr: "the
  running job keeps its current definition until restarted".
- **D23.**
  - `TestEnvOverridesTheLimitsAndSandboxKeys`: setting
    `MCREMOTE_LIMITS_WS_READ_DEADLINE_SECONDS`, `MCREMOTE_LIMITS_WS_RESUME_WINDOW_SECONDS`,
    `MCREMOTE_LIMITS_TCP_KEEPALIVE_DISABLE`, and the other `tcp_keepalive` leaves, and
    `MCREMOTE_PROVIDERS_CODEX_SANDBOX_BROKEN_POLICY` changes the loaded `Config`.
  - `TestEveryConfigKeyHasAViperDefault`: reflection over `Config`'s `mapstructure` leaves,
    against `v.AllKeys()` after `setDefaults`, with the allowlist `map[string]string` (key to
    reason) empty unless a key is deliberately unbound.
  - **Implement:** the missing `SetDefault` calls in `setDefaults`.
- **D24.** `TestRelativeDataDirFlagResolvesAgainstCWD` (`internal/cli/paths_test.go`, a new file): `paths
  --data-dir rel/x`, from a temporary working directory, reports `<tmp>/rel/x`, and `serve`'s
  path is the same through its pre-run. Remove the re-assignment blocks in `serve.go` and
  `paths.go`. Export `config.AbsDataDir(raw string) (string, error)`, and use it in `pair.go`'s
  parent-flag branch.
- **D25.** `TestOpenStoreCannotTruncateAConcurrentWrite` (`internal/auth/store_test.go`): 200
  rounds; a writer under `withPathLock` adds a device while `OpenStore` runs on a missing file.
  The device is present afterwards. In `OpenStore`, the stat and the create go inside
  `withPathLock`.
- **D27.** `TestUnitPinsRuntimeDirWhenInstallerHasNone` (`internal/cli/service`): both templates,
  rendered with an empty `XDGRuntimeDir`, contain `Environment=XDG_RUNTIME_DIR=%t`; with a value,
  they contain that value. Template: `{{if .XDGRuntimeDir}}…{{else}}Environment=XDG_RUNTIME_DIR=%t
  {{end}}`.
- **D28.** `TestAPanickingFetchReleasesItsWaiters` (`internal/picker/cache_test.go`): the fetch
  panics; the caller and one concurrent waiter both get an error within 100 ms; the next call
  fetches again. The cleanup goes into a `defer` that `recover`s, records a `fmt.Errorf("catalog
  fetch panicked: %v", r)`, deletes the in-flight entry and closes `done`.
- **D29.** `TestTakeLogsAStoreThatCannotWrite` (`internal/auth/paircode_test.go`): with the store
  file's directory made read-only after a code is created:
  - `Take` of an unknown code logs one `warn` and returns `ErrInvalidPairCode`;
  - `Take` of an expired code logs one `warn` and returns `ErrExpiredPairCode`;
  - both results are exactly as today.

  Both `_ = s.save(data)` sites in `Take` become a `warn` log, through a logger field on
  `PairCodeStore` that defaults to `slog.Default()`. The test skips on Windows, and when running
  as root, where a read-only directory can still be written.
- **D31.** In `scripts/smoke-protocol/main.go`: the auth payload sends `"protocols":[2,1]`; the
  `auth_ok` check requires `protocol == 2`; the empty block is deleted. Check: `go build
  ./scripts/smoke-protocol/` and `go vet ./scripts/...`. P10 runs it against a local daemon.
- **D32.** `TestDoctorReportsEachEnginesBinaryAndReadiness` (`internal/cli/doctor_test.go`): a
  fake registry with one ready provider and one whose binary is missing. The rendered report
  lists both, with "ready" and "missing: <bin>". It uses `Ready()` and `exec.LookPath`, and never
  `Start`.
- **Planted breaks:**
  - D22: the old unconditional `stopAndWaitDarwin`;
  - D23: one `SetDefault` removed, which must fail both tests;
  - D24: the `serve.go` re-assignment restored;
  - D25: the create outside the lock;
  - D27: the template's `else` branch removed;
  - D28: no `recover`;
  - D29: the `warn` log removed (`_ =` restored).

  Each must fail its test.

### P7 — Phone app (D33–D39)

Tests go in `apps/mobile/test/`, named after each file.

- **D33.** `transcript_cache_serial_test.dart`: two `TranscriptCache` instances on one temporary
  directory, with interleaved `save` and `clear`, leave an index that lists exactly the files on
  disk. It is seen failing with the per-instance chain. `_serial` becomes `static`.
- **D34.** `connect_host_edit_test.dart`: with a pending pair hint shown, entering text in Host
  removes the hint after one `pump()`, with no 600 ms wait.
- **D35.** `connect_paste_double_tap_test.dart`: two taps on Paste, 10 ms apart, with a clipboard
  holding a pair URI. The fake client records one claim.
- **D36.** `chat_bubble_post_frame_test.dart`: a streaming bubble switched to finished between the
  frame and the post-frame callback renders as final, with no caret.
- **D37.** `settings_clear_secrets_test.dart`: a store whose `clearSecrets` throws
  `SecureStorageUnavailable` shows an error snackbar.
- **D38.** `notification_service_init_test.dart`: two concurrent `init()` calls initialise the
  plugin once, which the fake counts.
- **D39.** `app_update_cleanup_test.dart`: the download stream throws, and `sink.close` throws.
  The partial file is gone, and the error rethrown is the stream's.
- **Gates:** `dart format` on the changed files, then `make preflight`. Its analyze, test and
  format must all be clean.
- **Planted breaks:**
  - D33: the instance `_serial` restored;
  - D35: the early `_busy = false` restored;
  - D39: the old `catch` order restored.

  Each must fail its test.

### P8 — Records

1. Each finding's row in `0073-REPORT-triage-of-the-non-goose-findings.md` gets "FIXED by
   `<commit>` (0201 Dn)", as its phase lands.
2. `0090-PLAN-config-template-completeness.md` Phase 3 Task 3.1 gets a dated note: done by 0201
   D23 (`<commit>`).
3. `docs/guides/protocol-v1.md` gains D9's sentence and D10's error code, in their phases;
   `protocol-v2.md` too, where the same text appears.

### P9 — After 0054's S1 and S3 (D26, D30, the §6.1 check)

This phase starts only when 0054 is accepted, and its S1 (library backends and reconciler) and
S3 (the library command) are committed.

1. **D26.**
   - `TestUpdateWarnsWhenTheServiceRunsAnotherBinary`, driven by `selfupdatetest`: a refresh child
     whose receipt carries `warnings: ["the definition runs /x, not this binary (/y)"]`. The
     library-backed `update` writes `warning: the definition runs /x …` to stderr, and exits 0.
   - Record that the same scenario printed nothing on the `mcplib` path. Run that on a scratch
     copy of the commit before 0054's S1.
2. **D30.**
   - `TestControlUsesTheRecordedUnitName`: with `<state_dir>/service-unit` holding `custom`,
     every `systemd`/`launchd`/`taskscheduler` backend is built with `Name: "custom"`; without
     the file, with the product name.
   - `TestSetupRecordsAndRemoveDeletesTheUnitName`.
3. **§6.1.** `TestUpdateOnAHostWithNoServiceInstallsStandalone`: no service definition, so the
   update installs the binary, issues no start, and does no rollback. It holds after the move.
4. **Planted breaks:** D30, the resolver returning the product name only. It must fail its test.

### P10 — Real hosts

1. **D8.** `openssl s_client -alpn h2,http/1.1` against the daemon in self-signed mode on the Mac,
   and on the Linux test host, reports `ALPN protocol: http/1.1`.
2. **D22.**
   - On the Mac, with the service running: `mcremote setup-service --force --no-start` leaves
     `launchctl print` showing the same PID.
   - On the Windows test host, with the task stopped: `--no-start` leaves it stopped.
3. **D27.** On the Linux test host: `setup-service --print-only`, from an environment without
   `XDG_RUNTIME_DIR`, contains `Environment=XDG_RUNTIME_DIR=%t`, and the running service's admin
   socket is under `/run/user/<uid>/`.
4. **D31.** `go run ./scripts/smoke-protocol`, against a local daemon, reports v2.
5. Each host check is recorded with its output, redacted (`<user>`, "the Linux test host"). Any
   change made to a real host is undone, and that is recorded.

## Verification

The MADR's Confirmation:

- every decision's test seen failing and then passing;
- every planted break failing;
- every phase's gates;
- P10's real-host checks.

The PLAN is `complete` when every row of the triage report that it covers is marked FIXED, and P9
and P10 are done.

## Rollout and Rollback

- **Each phase is one commit** and reverts on its own. P3 and P4 change lock ordering, so they
  are reverted together if either is.
- **D14's `LastSeq` is additive.** Old records without it seed from history, as today.
- **D17's reset** affects only sessions cleared after it lands.
- **D23 only adds environment bindings.** A user whose environment happened to set those
  variables, which were ignored before, now gets them applied. P8 adds a line about it to the
  release notes.
- **The phone changes (P7) are self-contained,** and need no daemon change.

## Execution record

Approved 2026-10-18: the owner committed the records (`98e19ddc`) and said "committed, proceed".

### P0, 2026-10-18: baseline, and F3(a) reproduced

Starting commit `98e19ddc`.

**Step 1.** `go test -race -count=1 ./...` gave exit 0, with 42 packages `ok`.

**Step 2, F3(a).** A scratch daemon was built from `98e19ddc` and run on `127.0.0.1:7542`, in
`selfsigned` mode, with `auth.require_client_key: false`, only the `fake` provider enabled, and
every `XDG_*` directory in the session scratch directory (data directory A). The script asserted
nothing on the tree. It built `mcremote` and `scripts/smoke-protocol`, then:

```text
## token written to data dir B, daemon on A: exit=1
     ✗ hello: authenticated GET returned HTTP 401
## token written to data dir A (the daemon's): exit=0
     ✓ smoke complete (--skip-prompt)
```

- **The cause is confirmed.** F3(a) is the data-directory split, as D2 assumes. A token written to
  any other data directory is rejected, and one written to the daemon's own is accepted.
- **F3(b)'s silence was seen too.** The daemon's log holds a line for the accepted device, and
  nothing at all for the rejected one.
- **Teardown.** The scratch daemon was stopped, and its data, state, cache and the second data
  directory were removed.

### Deviation 1, 2026-10-18 (P1): D5 needs a tombstone, not `fail()`

**Found.** P1's D5 step says `writeJSON` calls `s.idem.fail(...)` for an error envelope. A handler
that writes an error frame almost always returns nil, and then `dispatchAsync`'s success path calls
`complete(nil)`. `fail()` deletes the entry, so that `complete(nil)` would re-create it as a
finished success with no frame, and the retry would get `retry_no_result` instead of running
again.

**Done instead.** There was no change to D5's decision, or to its behaviour.

- An error envelope marks the entry failed and keeps it (`failFrame`), so the later
  `complete(nil)` finds it finished and does nothing.
- `begin` replaces a failed entry with a fresh in-progress one before handing out the work, so the
  new attempt's success frame is captured and replayed like any other.

The test (`TestErrorFramesAreNotReplayedToARetry`) drives exactly that sequence: begin, an error
frame, then `complete(nil)`.

### P1, 2026-10-18: transport and authentication — complete

Starting commit `98e19ddc`. The tree's production changes are:

- **`internal/ws`:** `server.go`, `idempotency.go`, and the new `authlog.go` and `unowned.go`;
- **`internal/protocol/errors.go`:** `ErrSessionCancelTimeout`;
- **`internal/session/manager.go`:** `OwnerState`;
- **`docs/guides/protocol-v1.md`:** D9's sentence, D10's code, and D5's rule, placed after the
  error table.

**Each test seen failing first.** Where a test needed a type or variable to compile, the stub was
added alone first, so each failure below is an assertion, not a build error.

```text
D5  idem_errorframe_test.go:31: retry after an error frame: action 1 (frame "{...\"code\":\"turn_busy\"}}"), want execute
D9  bearer_upgrade_test.go:63: no frame after a Bearer upgrade: failed to get reader: context deadline exceeded
D10 cancel_bound_test.go:110: failed to get reader: context deadline exceeded        (10.03 s: the read loop was held)
D1  authlog_test.go:86: got 0 rejection records, want 5
D3  unowned_drop_test.go:73: after reset: 0 drop records, want 2 (ghost-3, and the purged session)
D6  slot_deadline_test.go:62: a ninth operation was refused after the deadline passed: {...rate_limited...}
```

**What changed.**

- **D1.** A per-(code, host) limiter (`authlog.go`) logs each rejection at `warn`, with `code`,
  `remote_addr` and, on a client-key failure, `device_id`, carried by a wrapping `deviceAuthError`
  that keeps `errors.Is` working. `authorizeHTTP` now returns its reason, and `/v1/hello` logs it.
  No token reaches the log; the test checks for any `mcr_`.
- **D3.** `Manager.OwnerState` distinguishes known, purged and unknown. `BroadcastEvent` logs a
  drop once per id (256 remembered), at `info`, or at `debug` for a purged session.
- **D5.** As in Deviation 1.
- **D6.** The slot is released once (`sync.Once`), at handler return or at the deadline plus
  `asyncSlotGrace` (5 s), and an overrun logs `warn`.
  - The deadline is now read once, before the goroutine starts. The first `-race` run caught the
    goroutine reading `asyncOpTimeoutFor` while the test restored it.
  - That is a real ordering fix: the context and the slot timer now share one value.
- **D9.** A successful Bearer upgrade sends a v1 `auth_ok`, and sets the connection to v1.
- **D10.** `session.cancel` waits at most `cancelBound` (10 s). Past it, the answer is
  `session_cancel_timeout` and a `warn`, and the call finishes on its own.

**A test fix of my own.** The throttle test first panicked because its client's 8-frame queue
filled up. The test client was given 64 frames; the code was not at fault.

**Planted breaks**, each on a fresh scratch copy. All six failed their test with an assertion:

```text
D1 limiter keyed without the address   authlog_test.go:86: got 4 rejection records, want 5
D3 drop not logged                     unowned_drop_test.go:73: after reset: 0 drop records, want 2
D5 error frames captured again         idem_errorframe_test.go:31: retry after an error frame: action 1 (...turn_busy...)
D6 no deadline release                 slot_deadline_test.go:62: a ninth operation was refused after the deadline passed
D9 no auth_ok on a Bearer upgrade      bearer_upgrade_test.go:63: no frame after a Bearer upgrade
D10 a plain blocking cancel            cancel_bound_test.go:110: failed to get reader: context deadline exceeded
```

**Gates.**

```text
make pre-add-check (every P1 Go file)   -> 21 file(s) clean (gofmt, golint, govulncheck), after golint's
                                           const-comment form was fixed on OwnerKnown/Purged/Unknown
go vet ./...                            -> exit 0
go test -race -count=1 ./...            -> exit 0, 42 packages ok
make lint, make staticcheck             -> exit 0
make ci-windows (the Windows test host) -> ci-windows-exit=0, ALL SELECTED CHECKS PASSED (ws, session, protocol ok)
check_records.py --check-all, make markdownlint-docs -> exit 0
```

The triage report's rows for F3(b), F-T1 (first half), F-T4, F-T5, F-T9 and the F-T3 residual are
marked when this phase's commit exists (P8).

### P2, 2026-10-18: relay and daemon TLS — complete

Starting commit `589ba066` (P1).

**Each test seen failing first:**

```text
D7  hub_control_deadline_test.go:78: join 1 never returned: writeControl has no deadline
D8  tls_alpn_test.go:59: negotiated "h2" with a client offering h2 first, want http/1.1
D8  acme_test.go:121: NextProtos = [h2 http/1.1 acme-tls/1], want http/1.1 first
```

D8's first line reproduces, as a unit test, the MADR's measurement on a scratch daemon: a
self-signed daemon picked `h2`.

**What changed.**

- **D7.** `writeControl` (`internal/relay/hub.go`) writes under `controlWriteTimeout` (10 s, the
  control ping's bound). A timeout reads as `errHostOffline`, so the join fails with
  `host_offline`, as any write failure did.
  - The test wedges a real connection: a writer floods a peer that never reads, until the
    connection's own write lock is held.
  - It then requires two joins to return within a second each.
- **D8.**
  - The daemon's `http.Server` is built by a new `newHTTPServer` (`internal/daemon/daemon.go`,
    extracted unchanged first). It sets `Protocols` to HTTP/1 only, so `ServeTLS` no longer adds
    HTTP/2 in any mode.
  - `ACMEBundle.TLSConfig` (`internal/certs/acme.go`) offers `http/1.1` first, then whatever
    certmagic adds, minus any `h2`/`h2c`.

**Deviation in naming only.** The PLAN named a new `TestACMEConfigOffersNoH2`. The existing
`TestACMEBundleTLSConfig` already asserted `h2` first, which is the very behaviour D8 reverses. So
that assertion was changed to the decided one: `http/1.1` first, and no HTTP/2 anywhere. It fails
on the old code (above), rather than a second test sitting beside a contradicting one.

**Planted breaks**, each on a fresh scratch copy. All three failed their test with an assertion:

```text
D7 no control-write deadline   hub_control_deadline_test.go:78: join 1 never returned
D8 Protocols left nil          tls_alpn_test.go:59: negotiated "h2" with a client offering h2 first
D8 ACME offers h2 again        acme_test.go:121: NextProtos = [h2 http/1.1 acme-tls/1], want http/1.1 first
```

**Gates.**

```text
make pre-add-check (6 Go files)        -> clean (gofmt, golint, govulncheck)
go vet ./..., make lint, make staticcheck -> exit 0
go test -race -count=1 ./...           -> exit 0, 42 packages ok
make ci-windows (the Windows test host) -> ci-windows-exit=0, ALL SELECTED CHECKS PASSED (certs, daemon, relay ok)
check_records.py --check-all           -> exit 0
```

The real-host ALPN check (`openssl s_client`) is P10.

