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
| P4 | D14, D15, D16, D17, D18, D19, D20 | `internal/session/manager.go`, `internal/session/commands.go`, `internal/session/store.go`, `docs/guides/protocol-v1.md` (Deviation 3) |
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
  - **Implement:** ~~`entry.claimMu`~~ the existing per-session lock (`lockCreate`), taken
    before `m.mu` in `Authorize`'s claim path. The check and the stamp happen under it.
    See Deviation 2.
- **Planted breaks:**
  - D11: the triggering `onEvent` moved back after the notices;
  - D13: ~~`claimMu`~~ the per-session lock removed (Deviation 2);
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
   classify as not quota. **Superseded by Deviation 4:** the first four rows below are replaced,
   because they already classify correctly and cannot fail first.
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

### Deviation 2, 2026-10-18 (P3): D13 uses the existing per-session lock, not a new `claimMu`

**Found.** P3's D13 step adds `entry.claimMu`. `Manager` already has a per-session lock,
`lockCreate(id)` (`internal/session/manager.go`), which `Create`, `Release` and `Claim` hold for the
same check, persist and stamp sequence. An `entry` field also cannot cover `Authorize`'s
stored-only path, where there is no entry: its `Get`, check and `Save` are unlocked too. Racing it
for 600 rounds under `-race` never caught a double win, but the window is there when read.

**Owner's decision.** Use the existing lock.

**Done instead.** MADR D13's decision holds as written: a per-session claim mutex is held across
check, persist and stamp, and is taken before `m.mu`.

- `Authorize` runs its checks without the lock. On reaching a first-touch claim, in either path,
  it returns an internal `errClaimNeedsLock`. `Authorize` then takes `lockCreate(id)` and runs the
  checks again, so the second claimer sees the first one's owner and is forbidden.
- Checks that claim nothing never take the lock. No caller of `Authorize` holds it, so it cannot
  deadlock.
- A claim is now also ordered against `Release` and `Claim`.
- `TestOnlyOneConcurrentClaimWins` covers both paths, with 200 rounds each: a `live` subtest, which
  failed first, and a `stored` subtest, which did not.
- P3's planted break (the lock removed) later caught a double win in the `stored` subtest as well,
  so that race is real, not only possible when read.

No files were added to the phase's scope.

### P3, 2026-10-18: session ordering and ownership — complete

Starting commit `bbd04664` (P2). D13 is as in Deviation 2.

**Each test seen failing first:**

```text
D4   create_id_test.go:54: Create = <nil>, want ErrSessionIDMismatch
D11  delivery_order_test.go:104: round 0: delivery 2 has seq 1 (session_mode) after seq 3 (remote_commands); want strictly increasing
D11  delivery_order_test.go:146: concurrent emits: delivery 20 has seq 19 (notice) after seq 21 (assistant_message_chunk); want strictly increasing
D12  replay_suppression_test.go:95: withheld-replay records = 0, want 1: []
D12  replay_suppression_test.go:119: during-turn warnings = 0, want 1: []
D13  claim_race_test.go:81: <id>: both claimers were told they own the session
```

**What changed.** All of it is in `internal/session/manager.go` and `internal/session/commands.go`.

- **D4.** `Create` refuses a provider session whose id is not the `LocalSessionID` it asked for. It
  closes that session and returns `ErrSessionIDMismatch`, so no entry exists under either id.
- **D11.**
  - `entry.deliverMu` is held from an event's seq stamp to the last client delivery it causes. The
    lock order is in its comment: `lockCreate`, then `deliverMu`, then `advertiseMu`, then `m.mu`.
    Two entries' `deliverMu` are never held at once.
  - The pump delivers the event itself first, then what it triggered: `advertiseCommandsLocked`
    and the context-pressure notice, through `emitEventLocked`. The history and meta persistence
    stay ahead of the broadcast, as before.
  - Global-budget notices may name other sessions, so they are emitted after the lock is released.
  - `emitEvent` and `advertiseCommands` take the entry's `deliverMu`. If the entry was replaced in
    between, `emitEventLocked` reports it, and `emitEvent` retries under the new entry's lock.
- **D12.**
  - `noteReplay` counts withheld replay events per session. A live event ends a burst and logs it
    at `debug` (`replay events withheld from live delivery`, `count`).
  - A replay event while a turn is in flight logs at `warn`, with its type, once per turn.
    `markPromptStart` re-arms it.
  - `Create` flushes a burst still being counted at its return.
- **D13.** See Deviation 2.

**Deviations in test detail only.**

- `TestFirstEventsOfASessionArriveInSeqOrder`:
  - **The PLAN said:** record "the first 20 events".
  - **Why it changed:** a fake session's start-up and first turn deliver 13. Waiting for 20 timed
    out, a failure for the wrong reason.
  - **What it does instead:** it waits for the turn's `turn_complete` and checks every delivery
    up to it, over 20 fresh sessions.
- `TestConcurrentEmitsKeepSeqOrder`:
  - **The PLAN said:** 4 goroutines calling `emitEvent`.
  - **What it does instead:** 4 goroutines × 50 `emitNotice` calls (a thin wrapper over
    `emitEvent`) against 200 provider events.
- `TestOnlyOneConcurrentClaimWins` adds a `stored` subtest (Deviation 2).
- **Not covered by a test:** `Create`'s flush of an unfinished burst. A test cannot tell it apart
  from the flush the next live event makes, because the pump runs concurrently with `Create`'s
  return.

**Planted breaks**, each on a fresh scratch copy. The PLAN's three, plus one for `emitEvent`'s lock
and one for each D12 log. All six failed their test with an assertion:

```text
D11 triggering onEvent after the notices  delivery_order_test.go:104: round 0: delivery 2 has seq 2 (session_mode) after seq 3 (remote_commands)
D11 emitEvent without the delivery lock   delivery_order_test.go:146: concurrent emits: delivery 37 has seq 36 (notice) after seq 41 (notice)
D12 burst count never logged              replay_suppression_test.go:95: withheld-replay records = 0, want 1
D12 no warning during a turn              replay_suppression_test.go:119: during-turn warnings = 0, want 1
D13 per-session lock removed              claim_race_test.go:81 (live) and :93 (stored): both claimers were told they own the session
D4  equality check removed                create_id_test.go:54: Create = <nil>, want ErrSessionIDMismatch
```

**Gates.**

```text
make pre-add-check (6 Go files)          -> clean (gofmt, golint, govulncheck)
go vet ./..., make lint, make staticcheck -> exit 0
go test -race -count=1 ./...             -> exit 0, 42 packages ok
P3 tests, -race -count=10                -> exit 0
make ci-windows (the Windows test host)  -> ci-windows-exit=0, ALL SELECTED CHECKS PASSED (session ok)
check_records.py --check-all             -> exit 0
make markdownlint-docs                   -> exit 0
```

### Deviation 3, 2026-10-18 (P4): the protocol guide documents `last_seq` and turn-end cancellation

**Found.** P4 changes two things clients can see, and no phase documented either. P8 adds only
D9's sentence and D10's error code to the guides.

- D14 gives `Meta` a `last_seq` field, with the PLAN's tag `last_seq,omitempty`. `Meta` is also
  the wire shape of a session row, so clients receive it. The guide's list of optional session
  fields did not name it.
- D20 sends `permission_resolved` and `question_resolved` with `cancelled` when a turn ends.
  The guide's `cancelled` text gave the causes as an agent-side cancel, a session close, and a
  timeout.

**Owner's decision.** Document both in P4.

**Done instead.** `docs/guides/protocol-v1.md` joins P4's scope (the table above is annotated).

- It gains a `last_seq` bullet among the optional session fields.
- Its `cancelled` text names the turn's end as a cause and describes the turn-end events.

`protocol-v2.md` carries neither passage, so it is unchanged. No decision in the MADR changes.

### P4, 2026-10-18: session persistence — complete

Starting commit `6e2e30be` (P3). The guide changes are as in Deviation 3.

**Test seams, added alone first, with no change in behaviour:**

- `afterHistorySnapshotHook`, called in `FlushHistory` between a session's snapshot and its write.
  It is an `atomic.Pointer`: the first green run showed a data race between a test restoring a
  plain variable and a timer's flush still reading it.
- `historyMaxLatency`, changed from a `const` to a `var`.

**Each test seen failing first.** `TestModelSwitchKeepsTheTranscript` is a pin, and passed, as a
pin should.

```text
D14  persistence_0201_test.go:138: first event after re-create has seq 1 (remote_commands); seqs up to 13 were already sent
D15  persistence_0201_test.go:186: a stale flush overwrote the close transcript: disk has 6 events, the close wrote 10
D15  persistence_0201_test.go:222: session directory after Delete and a late flush: stat err = <nil>, want not-exist
D16  persistence_0201_test.go:277: second Create = <nil> after 1.001519542s; want context.DeadlineExceeded within 300ms
D17  persistence_0201_test.go:306: cold history kept seq 1 (remote_commands "") from before /clear (last pre-clear seq 27)
D18  persistence_0201_test.go:389: chunk 0 reached clients 2.044815875s after the provider sent it; want under 50ms
D19  persistence_0201_test.go:412: notice for a closed session went out with seq 0; want 14
D19  persistence_0201_test.go:429: a notice for an unknown session was broadcast: 1 deliveries
D20  persistence_0201_test.go:475: p:p-open: 0 resolved events at turn end, want 1
```

**What changed.** The code is in `internal/session/manager.go`, `commands.go` and `store.go`.

- **D14.** `Meta` and `Record` gain `LastSeq`.
  - The close path sets it from the entry's seq and persists the meta whether the transcript
    save succeeded or not.
  - `Create` seeds seq from the higher of the transcript's last seq and the record's `LastSeq`,
    and carries the result in the live meta.
- **D15.** Every transcript write goes through `saveHistory` / `saveHistoryLocked`, under
  `historyWriteMu`: flush, close, `/clear`, and D19's stored notices.
  - It skips a purged session, and a snapshot whose generation is not above `savedHistGen[id]`.
  - Record deletion takes the same mutex (`deleteStored`), including `writePersist`'s removal of
    a record a racing save left behind.
  - **The generation counter.** `entry.histGen` takes a fresh value from one package-wide counter,
    `histGenSeq`, on each append and each trim or deletion (`reindexHistoryLocked`).
  - The PLAN read as a counter per entry. That would restart at zero for a re-created entry, and
    `savedHistGen`, keyed by id, would then skip the new entry's writes. So would the writes of
    a stale snapshot taken from a replaced entry.
- **D16.**
  - `createLock` is a one-slot channel. `lockCreate(ctx, id)` returns `ctx.Err()` when the
    caller's context ends first, and `Create` returns that error.
  - `Authorize`, `Release` and `Claim` have no caller context. They pass
    `context.Background()`, so their wait is bounded by the holder, whose `Start` runs under its
    own caller's deadline. They still propagate the error.
- **D17.**
  - An internal `createOptions` carries `resetHistory`, and absorbs `create`'s existing
    name-source argument (Fork's).
  - `relaunch` takes `reset`. `cmdReset` passes `true`; the model switch passes `false`.
  - With `reset`, `Create` seeds an empty ring and writes the empty transcript through D15's
    helper. Seq still continues from D14's seeding.
  - `relaunch`'s doc comment now says what happens.
- **D18.** Past the latency bound, `scheduleHistoryPersist` fires the history timer at once:
  `Reset(0)`, or `AfterFunc(0, …)` when there is no timer. The flush no longer runs on the
  pump's goroutine.
- **D19.** `emitEvent` for a session with no live entry goes to `emitStoredEvent`.
  - **With a stored record:** the event takes `max(LastSeq, transcript tail) + 1`, joins the
    transcript through D15's helper, and is broadcast under `historyWriteMu`, so two of them
    keep seq order. The record's `LastSeq` is updated even if the transcript write fails.
  - **With none:** the event is dropped with a `debug` log.
  - `Create` holds `historyWriteMu` from its transcript load to its insert, so a stored notice
    cannot land in between.
- **D20.** At a live `turn_complete` or `error`, each ask still pending yields a
  `permission_resolved` / `question_resolved` with status `cancelled`.
  - The order is permissions then questions, each in id order.
  - They are emitted after the event itself, under D11's delivery lock.
  - A replayed turn end emits none.

**Deviations in test detail only.**

- **D14:** a real `Store` cannot be told to fail, so the test makes `history.json` a non-empty
  directory. Every transcript write then fails, and every read finds nothing, on POSIX and on
  Windows alike.
- **D18:**
  - **The PLAN said:** a store whose `SaveHistory` blocks.
  - **What it does instead:** the snapshot hook sleeps 2 s, which blocks the flush at the same
    point.

**Planted breaks**, each on a fresh scratch copy. The PLAN's six, plus a second one for D15, one
for each half of D19, and one that breaks the D17 pin. All ten failed their test with an
assertion:

```text
D14 seeding from history only      :138 first event after re-create has seq 1 (remote_commands); seqs up to 13 were already sent
D15 generation check removed       :184 a stale flush overwrote the close transcript: disk has 6 events, the close wrote 10
D15 purge check removed            :220 session directory after Delete and a late flush: stat err = <nil>, want not-exist
D16 select on ctx removed          :275 second Create = <nil> after 1.00269825s; want context.DeadlineExceeded within 300ms
D17 resetHistory ignored           :304 cold history kept seq 1 (remote_commands "") from before /clear (last pre-clear seq 27)
D17 every relaunch resets          :335 a model-switch relaunch dropped the transcript: 7 events, none the earlier turn
D18 inline flush restored          :387 chunk 0 reached clients 2.019455542s after the provider sent it; want under 50ms
D19 stored notice unsequenced      :410 notice for a closed session went out with seq 0; want 14
D19 unknown session broadcast      :427 a notice for an unknown session was broadcast: 1 deliveries
D20 no synthetic events            :473 p:p-open: 0 resolved events at turn end, want 1
```

(All in `persistence_0201_test.go`. Line numbers differ from the first-failure list because an
unused leftover was removed from the D15 test in between.)

**Gates.**

```text
make pre-add-check (4 Go files)          -> clean (gofmt, golint, govulncheck)
go vet ./..., make lint, make staticcheck -> exit 0
go test -race -count=1 ./...             -> exit 0, 42 packages ok
P3 and P4 tests, -race -count=5          -> exit 0
make ci-windows (the Windows test host)  -> ci-windows-exit=0, ALL SELECTED CHECKS PASSED (session ok)
check_records.py --check-all             -> exit 0
make markdownlint-docs                   -> exit 0 (covers protocol-v1.md)
```

### Deviation 4, 2026-10-18 (P5): D21's table uses cases that reach the hard-quota list

**Found.** `Classify` consults `hardQuotaWords` only when a message matches both a quota phrase
and a rate phrase (`internal/agenterr/agenterr.go`, the `quota && rate` case). Four of the PLAN's
six cases have no quota phrase, so they never reach that list.

A probe on a scratch copy at `d53c4f3b` showed this:

```text
"the explanation of rate limit 429"                            -> rate_limit
"account suspended: rate limit"                                -> rate_limit
"accredited endpoint: rate limit"                              -> rate_limit
"planned maintenance: too many requests"                       -> rate_limit
"context length limit reached"                                 -> quota
"maximum token limit reached"                                  -> quota
"rate limit reached; see the explanation in the docs"          -> quota
"rate limit reached: account suspended for 60s"                -> quota
"rate limit reached for accredited partner endpoints"          -> quota
"too many requests: limit reached during planned maintenance"  -> quota
```

The first four are already right. They cannot fail first, and the planted break (the substring
list restored) would pass them.

**Owner's decision.** Swap in red cases.

**Done instead.** The table keeps the two "limit reached" rows, which expect no class. It replaces
the first four with the last four above, which expect `rate_limit`.

- Each row asserts the exact kind, not only "not quota".
- All six fail on the old code.

The MADR's decision is unchanged.

### P5, 2026-10-18: quota classification — complete

Starting commit `d53c4f3b` (P4). CI run 39189165942 on that commit passed before P5 began. The
test table is as in Deviation 4.

**The test seen failing first.** All six rows failed:

```text
agenterr_test.go:484: Classify("rate limit reached; see the explanation in the docs") = "quota", want "rate_limit"
agenterr_test.go:484: Classify("rate limit reached: account suspended for 60s") = "quota", want "rate_limit"
agenterr_test.go:484: Classify("rate limit reached for accredited partner endpoints") = "quota", want "rate_limit"
agenterr_test.go:484: Classify("too many requests: limit reached during planned maintenance") = "quota", want "rate_limit"
agenterr_test.go:484: Classify("context length limit reached") = "quota", want ""
agenterr_test.go:484: Classify("maximum token limit reached") = "quota", want ""
```

**What changed** (`internal/agenterr/agenterr.go`):

- `hardQuotaWords` keeps only its phrases. The single words match through `reHardQuotaWord`,
  `\b(billing|credits?|quota|spend|spending|plan|plans|weekly|monthly)\b`, compiled once.
  `isHardQuota` checks both.
- `limit reached` leaves `quotaWords`. `isQuota` counts it per sentence (split on `.`, `!`, `?`
  and newlines), unless that sentence matches `\b(context|tokens?|length)\b`.
- **The corpus.** Every existing classification test passed unchanged, with no edits. That
  covers `internal/agenterr` and the `codex` and `acpagent` provider tests that classify through
  it.

**Planted breaks**, each on a fresh scratch copy. The PLAN's, plus one for the exemption. Both
failed the table with assertions:

```text
D21 old substring list restored             the four rate_limit rows: = "quota", want "rate_limit"
D21 context/token/length exemption removed  the two limit rows: = "quota", want ""
```

**Gates.**

```text
make pre-add-check (2 Go files)          -> clean (gofmt, golint, govulncheck)
go vet ./..., make lint, make staticcheck -> exit 0
go test -race -count=1 ./...             -> exit 0, 42 packages ok
make ci-windows (the Windows test host)  -> ci-windows-exit=0, ALL SELECTED CHECKS PASSED (agenterr ok)
check_records.py --check-all             -> exit 0
make markdownlint-docs                   -> exit 0
```

### Deviation 5, 2026-10-18 (P6): D23's allowlist names ten deliberately unbound keys

**Found.** The reflection test walks every `mapstructure` leaf of `Config`, embedded `,squash`
structs included. It found 17 keys with no viper default: the MADR's seven, and ten more.

| Key | Kind | Why it has no default |
| --- | --- | --- |
| `tls.letsencrypt.domains` | string list | config file or `--tls-domain` only |
| `providers.grok.args` | string list | config file only |
| `providers.grok.fs_roots` | string list | config file only |
| `providers.grok.allowed_tools` | string list | config file only |
| `providers.grok.disallowed_tools` | string list | config file only |
| `providers.grok.allow_rules` | string list | config file only |
| `providers.grok.deny_rules` | string list | config file only |
| `providers.grok.mcp_servers` | list of tables | config file only; `load.go` already says so |
| `providers.codex.environments` | list of tables | config file only |
| `providers.opencode.transport` | retired key | `Config.validate` rejects it; a default would make every config look as if it set it |

A comma-split environment value cannot carry list entries that contain commas, such as `args`, or
rules like `Bash(git log, …)`. A list of tables cannot be expressed in one variable at all.

**Owner's decision.** List all ten as deliberately unbound.

**Done instead.** `setDefaults` gains defaults for the MADR's seven keys only. The test's
`unboundKeys` names the ten, each with its reason, and any new key without a default still fails
the test. The MADR's decision is unchanged: every key that can be set from the environment now
is.

### Deviation 6, 2026-10-18 (P6): D27 also teaches refresh to read `%t` back

**Found.** With the D27 template change in place, four existing tests failed.

1. **`TestRefreshRoundTripsRenderedUnit` (all four cases) and `TestRefreshUnchangedOnFreshSetup`
   (`internal/cli/service/refresh_test.go`).** Refresh reads an installed unit's
   `Environment=XDG_RUNTIME_DIR=%t` back as the literal value `%t`. The re-render then escapes
   it as data, as `%%t`, which is a literal path and no longer the specifier. So a fresh setup
   reported `refreshed` instead of `unchanged`, and every refresh would have rewritten the unit
   with a broken runtime directory.
2. **`TestRenderOmitsListenAndRuntimeDirWhenUnset` (`internal/cli/service/setup_test.go`).** It
   asserted the omit-when-empty rule that D27 reverses, "empty XDG_RUNTIME_DIR must be omitted
   (it would override systemd's own)". `%t` is systemd's own value: for a user manager,
   systemd.unit(5) defines it as the path `$XDG_RUNTIME_DIR` resolves to.

**Owner's decision.** Fix refresh, and update the test.

**Done instead.** `internal/cli/service/refresh.go` joins P6's scope.

- `assignManagedEnv` reads an installed value of exactly `%t` back as "no installer value", which
  the template renders as `%t`. The four refresh tests then pass unmodified.
- The setup test asserts the decided behaviour: with an empty installer value, the unit carries
  exactly one line, `Environment=XDG_RUNTIME_DIR=%t`.

The MADR is unchanged.

### Deviation 7, 2026-10-18 (P6): D32 extracts a side-effect-free provider builder from the daemon

**Found.** D32's step has `doctor` call each enabled provider's `Ready()` through a registry. But
the real registry is built only inside `daemon.Run` (`internal/daemon/daemon.go`), interleaved
with:

- orphan-engine reaping;
- credential-guard recovery and coordinated wrappers;
- the "not found in PATH" warnings;
- prewarm.

`doctor` could not build one without that wiring. Every real `Ready()` is
`launch.Resolve(bin)`, and codex's also returns false on a configuration error.

The four provider constructors (`grok.NewWithLogger`, `opencode.NewHTTPWithLogger`,
`codex.NewWithLogger`, `kilo.NewHTTPWithLogger`) start no goroutine or process, and write no file.

**Owner's decision.** Extract a registry builder.

**Done instead.** `internal/daemon/daemon.go` joins P6's scope.

- Each provider's configuration translation moves into one helper. `daemon.Run` and the new
  builder share them, so the two cannot drift.
- The new builder is exported, `daemon.InspectProviders`. It builds the enabled providers with no
  coordination, no engine and no prewarm, and starts nothing.
- `daemon.Run` keeps its own wiring: coordinators, warnings and prewarm. Its behaviour is
  unchanged.
- `doctor` lists each provider from `InspectProviders`. It prints `ready` and the resolved path,
  `missing: <bin>` when the binary does not resolve, or `not ready` when the binary resolves but
  the provider still says no (codex's configuration check).

The MADR's decision is unchanged.

### P6, 2026-10-18: CLI, configuration, store and installer — complete

Starting commit `bbd2d154` (P5). D23 is as in Deviation 5, D27 as in Deviation 6, and D32 as in
Deviation 7.

**Each test seen failing first:**

```text
D2   pair_daemon_test.go:63: no warning; stderr: (empty)
D22  nostart_launchd_test.go:73: --no-start ran launchctl bootout gui/<uid>/com.magiccliremote.mcremote
D22  nostart_launchd_test.go:83: no note that the running job keeps its old definition
D22  nostart_schtasks_test.go:34: --no-start ran schtasks /run /tn mcremote
D23  viper_defaults_test.go:87: config keys with no viper default … (the MADR's seven, once Deviation 5's ten were listed)
D23  env_limits_test.go:39: limits.ws_read_deadline_seconds = 120, want 300 from the environment (and the other six)
D24  paths_test.go:37 / :56: paths and serve --data-dir rel/x: appdirs: data dir must be absolute, got "rel/x"
D25  store_open_test.go:55: round 0: 0 devices after a write raced OpenStore; the device was truncated away
D27  runtime_dir_test.go:32: mcremote and mcrelay units, installer XDG_RUNTIME_DIR="": no "Environment=XDG_RUNTIME_DIR=%t" line
D28  cache_test.go:191: caller: panicked=boom err=<nil>; want an error, not a panic
D29  paircode_save_test.go:50: after Take(unknown) on a store that cannot write: 0 warn lines, want 1
D32  doctor_engines_test.go:50 / :53: alpha wants ready and its path; beta wants missing: mcremote-no-such-engine
```

**Test seams, added alone first, with no change in behaviour:**

- **D29:** `PairCodeStore.log`, defaulting to `slog.Default()`.
- **D24:** `serve`'s config handling, extracted unchanged into `loadServeConfig(cmd)`, which reads
  its flags by name.
- **D32:** an empty `renderEngineDoctor`.

**What changed.**

- **D2** (`internal/cli/pair.go`). After writing a code (`pair`, `pair code`) or a token
  (`pair create`), `warnIfDaemonMissesDataDir` pings the admin socket. The socket path is keyed by
  the data directory. If nothing answers, and something accepts a TCP connection on the listen
  address (a wildcard host is dialled on loopback), it prints the warning to stderr. If nothing
  listens at all, it says nothing.
- **D22** (`internal/cli/service/setup.go`, `setup_schtasks.go`). Under `--no-start`:
  - **launchd:** the teardown is skipped. A running job whose plist changed gets one stderr
    note: "the running job keeps its current definition until restarted".
  - **schtasks:** the task is registered and not started.
- **D23** (`internal/config/load.go`). Seven `SetDefault` calls. The reflection test walks
  `Config`, including embedded `,squash` structs. The first draft of the walker skipped those,
  and missed grok's keys.
- **D24** (`internal/cli/serve.go`, `paths.go`, `pair.go`, `internal/config/load.go`).
  - `serve` and `paths` drop the re-assignment of `--data-dir`.
  - `config.AbsDataDir` is exported, and `pair`'s parent-flag branch uses it.
- **D25** (`internal/auth/store.go`). `OpenStore`'s stat and create run under `withPathLock`, and
  the file is created only if still absent.
- **D27** (both unit templates, `setup.go`, and `refresh.go` per Deviation 6). An empty installer
  value renders `Environment=XDG_RUNTIME_DIR=%t`.
- **D28** (`internal/picker/cache.go`). The leader's release runs in `lead`'s deferred function,
  and a panic becomes `catalog fetch panicked: …` for the caller and every waiter.
- **D29** (`internal/auth/paircode.go`). Both ignored saves go through `saveOrWarn`, which logs
  the path and the error at `warn`. The results are unchanged.
- **D31** (`scripts/smoke-protocol/main.go`).
  - Auth offers `protocols: [2, 1]`, and fails unless `auth_ok.protocol == 2`. Frames keep
    `v: 1`, which protocol-v2.md's accept rule allows.
  - The empty block is gone, and the header comment no longer says v1.
- **D32** (`internal/daemon/daemon.go`, `internal/cli/doctor.go`).
  - **daemon:** `grokConfig`, `opencodeConfig`, `codexConfig` and `kiloConfig` hold the
    translations. A line-by-line check found that every line removed from `daemon.Run` reappears
    in them. The six exceptions are the expected rewrites: the version parameter, and
    constructor calls turned into returns.
  - **builder:** `daemon.InspectProviders` builds the uncoordinated providers.
  - **doctor:** a new "agent engines" section, with `ready <path>`, `missing: <bin>`, or
    `not ready`. On this macOS host it listed codex, grok, kilo and opencode as ready, with their
    resolved paths.

**Deviations in test detail only:**

- **Test files.** Several tests live in new files rather than the ones the PLAN named:
  - D29's in `paircode_save_test.go`, in-package, because it swaps the logger;
  - D25's in `store_open_test.go`, in-package;
  - D2's in `pair_daemon_test.go`;
  - D23's two in `viper_defaults_test.go` (in-package) and `env_limits_test.go`;
  - D22's in `nostart_launchd_test.go` and `nostart_schtasks_test.go`;
  - D27's in `runtime_dir_test.go`;
  - D32's in `doctor_engines_test.go`.
- **D25's race.** A first draft raced two full `OpenStore`+`Create` sequences, and did not lose
  the device in 200 rounds: the writer's two fsync'd writes always outlasted the bare `OpenStore`.
  The test therefore uses the writer the PLAN describes, one atomic write under `withPathLock`,
  and the old code lost the device in round 0.
- **D22's launchd test** writes its placeholder binary with `testexec.WriteShellStub`.
  `TestRunFixturesAreWarmed` (MADR 0193) failed the first full race run on a hand-written
  `#!/bin/sh` file.

**Not covered by a test:**

- `pair create`'s call of the D2 warning; the `pair code` path is tested.
- `pair`'s parent-flag `--data-dir` branch.
- D31, which is built and vetted only. P10 runs it against a daemon.

**Planted breaks**, each on a fresh scratch copy. The PLAN's seven, plus one each for D22's
Windows half, D2 and D32. All ten failed their test with an assertion:

```text
D22 unconditional stopAndWaitDarwin     nostart_launchd_test.go: --no-start ran launchctl bootout gui/<uid>/…
D22 Windows task started regardless     nostart_schtasks_test.go:34: --no-start ran schtasks /run /tn mcremote
D23 one SetDefault removed              both: viper_defaults_test.go:87 and env_limits_test.go:39 (ws_resume_window_seconds = 120, want 600)
D24 serve re-assignment restored        paths_test.go:56: serve --data-dir rel/x: appdirs: data dir must be absolute
D25 create outside the lock             store_open_test.go:55: round 0: 0 devices … truncated away
D27 template else removed               runtime_dir_test.go:32: no "Environment=XDG_RUNTIME_DIR=%t" line
D28 no recover                          cache_test.go:191: caller: panicked=boom err=<nil>
D29 warn log removed                    paircode_save_test.go:50: 0 warn lines, want 1
D2  no warning call in pair code        pair_daemon_test.go:63: no warning
D32 doctor never says missing           doctor_engines_test.go:53: beta: want missing: mcremote-no-such-engine
```

**Gates.**

```text
make pre-add-check (25 Go files)                  -> clean (gofmt, golint, govulncheck)
go vet ./..., ./scripts/..., GOOS=windows ./internal/... -> exit 0
make lint, make staticcheck                       -> exit 0
go build ./scripts/smoke-protocol/                -> ok
go test -race -count=1 ./...                      -> exit 0, 42 packages ok
make ci-windows (the Windows test host)           -> ci-windows-exit=0, ALL SELECTED CHECKS PASSED
check_records.py --check-all                      -> exit 0
make markdownlint-docs                            -> exit 0
```

0090-PLAN's note for D23 is P8 step 2.

