---
status: accepted
date: 2026-10-18
decision-makers: Project Owner
consulted: none
informed: contributors who work on the daemon, the relay, the CLI and the phone app
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Fix every open finding from 0073's debug pass

## Context and Problem Statement

[0073-MADR-goose-prompt-hang-and-debug-pass.md](0073-MADR-goose-prompt-hang-and-debug-pass.md)
(2026-08-05) recorded a Goose prompt hang, plus about 30 findings from a code audit of the
transport, session, CLI and Flutter layers. Only its F1 and F2, and its §10 follow-up, were ever
fixed under its own name. Goose went with
[0160-MADR-remove-goose-cli-support.md](0160-MADR-remove-goose-cli-support.md).

On 2026-10-18 every other finding was triaged against `master` `56ea5621`:
[0073-REPORT-triage-of-the-non-goose-findings.md](../reports/0073-REPORT-triage-of-the-non-goose-findings.md).
Of its 46 rows:

- 7 were fixed by later records;
- 2 are obsolete (§6.8 and §6.10(c));
- 36 are still present;
- 1, F3(a), is unclear.

The owner decided the same day that **all of them are to be fixed**. This record decides how, one
finding at a time.

### What the triage did not know

Three facts were measured or read for this record, after the triage:

- **The daemon offers HTTP/2 in every TLS mode, not only Let's Encrypt.** The triage, and 0073
  before it, said the self-signed default was unaffected. A scratch daemon from this tree, in
  `selfsigned` mode on loopback, answered `openssl s_client -alpn h2,http/1.1` with `ALPN protocol:
  h2`. The cause is that `internal/daemon/daemon.go` serves with `http.Server.ServeTLS`, and a
  server whose `Protocols` and `TLSNextProto` are both nil serves HTTP/1 and HTTP/2 (`go doc
  net/http Server.Protocols`, Go 1.27.2). The relay is unaffected: it wraps its own TLS listener
  and strips `h2` (`internal/relay/server.go`, 0091 D10).
- **Windows ignores `--no-start` altogether.** 0073 §6.2 found that on macOS `setup-service
  --no-start` stops a running job and never restarts it. On Windows, `setupSchtasks`
  (`internal/cli/service/setup_schtasks.go`) never reads `NoStart`, and always starts the task.
  Only Linux honours the flag, by skipping `systemctl start` and stopping nothing.
- **F3(a)'s guessed cause could not have happened.** 0073 guessed that the daemon cached
  `devices.json`. But `Store.Validate` re-reads the file under the cross-process lock on every
  call, and has since before 0073 was written (`e48b208a`, 2026-07-22). Two causes remain:
  - the §6.7 race, in which `OpenStore` creates the file outside that lock;
  - a CLI and a service using different data directories. That is invisible today, because the
    admin socket's path derives from the data directory, so a CLI with a different directory
    simply finds "no daemon".

### Scope

Each finding still present is in scope, along with F3(a) and the one residual the triage left
under F-T3: `session.cancel` still runs on the read loop with no deadline. That makes 39
decisions in all.

### Relationship to 0054

Magic is to drop `mcplib` and adopt `go-selfupdate-lib`, for its update path and its service
backends:
[0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
whose MADR is `proposed` and whose S1–S8 have not started.

Two decisions here sit on code that migration replaces:

- **D26** (§6.6): the library already carries the warning that `mcplib` drops.
- **D30** (§6.10(d)): the library's backends take the unit name.

Both are fixed in the library-backed code, after 0054's S1, and not patched into `mcplib`-era code
first. One fix already done also rests on `mcplib`: §6.1, an update on a host with no installed
service. The migration must keep it.

Out of scope:

- the 7 findings already fixed, and the 2 obsolete;
- §6.11's remark that there is no "claude" engine. That observes the product's scope rather than
  a defect, and no record has ever planned one.

## Decision Drivers

- **Fix the defect, not its symptom.** A finding about something silent gets the missing signal,
  and the behaviour is fixed as well.
- **Each fix gets a test that fails on today's code,** seen failing first. The PLAN also plants
  breaks on scratch copies.
- **Do not break the wire protocol.** Every change keeps the phone (`apps/mobile`) and the
  protocol guides (`docs/guides/protocol-v1.md`, `protocol-v2.md`) compatible, or updates them in
  the same phase.
- **Keep each phase small enough to review and to revert** on its own.
- **Use what the code already has:**
  - `withPathLock` for cross-process file safety;
  - `declineUpdate`'s once-per-session logging pattern;
  - `PermissionStatusCancelled` for resolved events;
  - `Protocols` from the standard library.

## Considered Options

- One MADR with a decision per finding, and one PLAN phased by subsystem
- One MADR and PLAN per finding, or per small cluster
- Fix the six High findings now, and record the rest as accepted risk
- Document the behaviours instead of changing them

## Decision Outcome

Chosen option: "One MADR with a decision per finding, and one PLAN phased by subsystem". The owner
asked for every finding to be fixed. They share one source, and many touch the same files. One
record keeps the cross-cutting reasoning in one place: delivery ordering, history persistence,
and the shared session lock. The PLAN's phases are still small enough to review and revert one by
one.

The decisions follow. Each names the defect as it stands at `56ea5621`, the fix, and the test
that proves it. Severity is 0073's own ranking.

### Transport, authentication and relay

- **D1 — F3(b), S1: every rejected authentication is logged.**
  - **The defect.** `writeAuthError` (`internal/ws/server.go`) writes an `auth_error` frame for
    `invalid_token`, `client_key_required`, `client_key_mismatch` and `already_authed`, and logs
    nothing. `GET /v1/hello` returns 401 silently. A phone that cannot log in leaves no trace on
    the host.
  - **The fix.** Each rejection is logged at `warn`, with its code, the remote address, and the
    device id when one is known. No token, and no part of one, is ever logged. To keep a
    scanner from flooding the log, the same code from the same remote address is logged at most
    once a minute. Later repeats are counted, and the count goes on the next line.
  - **Test.** Each code produces exactly one `warn` line with no token text. A burst from one
    address produces one line plus a count.
- **D2 — F3(a), S1: a pair code written where no daemon is reading it says so.**
  - **The cause.** The cause 0073 guessed does not exist (Context). D25 removes the remaining
    race.
  - **The fix.** `pair create`, `pair code` and `pair` ping the admin socket for their own data
    directory. When nothing answers, they dial the configured listen address. If a daemon
    answers there, they print one warning: the code was written to `<data_dir>`, a daemon is
    listening on `<addr>`, and no daemon serves this data directory. A daemon that has not
    paired the device will reject the code. D1's log names the rejection on the daemon's side.
  - **Test.**
    - A CLI with a temporary data directory, against a listener on the configured port, prints
      the warning.
    - With the admin socket answering, it prints nothing.
    - The PLAN first tries to reproduce the original rejection, and records the outcome.
- **D3 — F-T1, High, first half: an event dropped because its session has no known owner is
  logged.**
  - **The defect.** `BroadcastEvent` returns silently when `OwnerOf` finds no owner.
  - **The fix.** The drop is logged at `info`, once per session id, with the event type. The set
    of ids already logged is bounded at 256, oldest first out, so the log cannot grow without
    end. A session that was purged on purpose is logged at `debug`, because dropping its late
    events is correct.
  - **Test.** Unknown sessions log once each. A purged session logs at `debug` only.
- **D4 — F-T1, High, second half: `Create` refuses a provider session whose id is not the one it
  asked for.**
  - **The defect.** `Manager.Create` keys the session and its `Meta.ID` by `sess.ID()`, and never
    checks it against `opts.LocalSessionID`. Every provider honours that id today. One that did
    not would orphan the owner record the transport looks up.
  - **The fix.** When `LocalSessionID` is set and `sess.ID()` differs, `Create` closes the new
    process and returns `ErrSessionIDMismatch`.
  - **Test.** A test provider that ignores the id is refused, and its process is closed.
- **D5 — F-T4, Medium: an error frame is never replayed as a request's result.**
  - **The defect.** `writeJSON` captures every frame that has an id into the idempotency
    ledger, error frames included. A retry of the same request within 10 minutes is then
    answered with the old error, `turn_busy` or a timeout among them, and the request never
    runs again.
  - **The fix.** An error envelope (`protocol.TypeError`) is not captured as the result. Instead
    the entry is marked failed, which is the rule `fail()` already applies when a handler
    returns an error. Its waiters re-begin once, as the F-T2 fix (`4d36964d`) arranged. Success
    frames are captured exactly as now.
  - **Test.** A request answered with `turn_busy` and retried with the same id executes again. A
    successful request is replayed, not re-executed.
- **D6 — F-T5, Medium: an async slot is returned when its operation's deadline passes.**
  - **The defect.** A slot is freed only when its handler returns. A handler that ignores its
    context keeps the slot for good, and eight such handlers turn every later operation on the
    connection into `rate_limited`.
  - **The fix.** The slot is released exactly once, whichever comes first:
    - the handler returns;
    - the operation's deadline (`asyncOpTimeout`) plus a 5 s grace passes.

    An overrun is logged at `warn` with the operation type and the elapsed time. The goroutine
    still finishes on its own. The shell lane's separate budget keeps the same rule.
  - **Test.** A handler that ignores its context frees its slot after deadline plus grace. A ninth
    operation is then accepted, and the slot is released once only.
- **D7 — F-T7, Med-Low: the relay's write to a host's control connection has a deadline.**
  - **The defect.** `writeControl` (`internal/relay/hub.go`) holds the slot's `writeMu` and
    writes under the phone's own request context, with no deadline. One stalled write blocks
    every join for that host.
  - **The fix.** The write runs under a 10 s deadline, the same bound as the control ping. A
    timeout returns `errHostOffline`, and the join fails with `host_offline`, as it does today.
  - **Test.** A control connection that never reads makes `writeControl` return within the
    bound, and a second join is not blocked behind the first.
- **D8 — F-T8, Low, daemon half: the daemon serves HTTP/1.1 only, in every TLS mode.**
  - **The defect.** As measured (Context), the daemon negotiates `h2` in self-signed mode as well
    as Let's Encrypt, and offers it first in the latter. It serves only WebSocket upgrades and
    small HTTP GETs, so HTTP/2 is unused surface.
  - **The fix.**
    - The daemon's `http.Server` sets `Protocols` to HTTP/1 only.
    - `ACMEBundle.TLSConfig` stops prepending `h2`. It offers `http/1.1` first, then whatever
      certmagic adds.
    - The relay keeps its `stripHTTP2`.
  - **Test.** A TLS client offering `h2,http/1.1` negotiates `http/1.1` against both a
    self-signed daemon and an ACME-config daemon. The test is run first against today's code
    and seen to negotiate `h2`.
- **D9 — F-T9, Low: authenticating with a Bearer header on upgrade gets an `auth_ok`.**
  - **The defect.** `Authorization: Bearer` on the WebSocket upgrade is documented as optional
    (`docs/guides/protocol-v1.md`). On success the socket is authenticated, but nothing is sent,
    so the client never learns it is authenticated, and the connection stays on v1.
  - **The fix.**
    - A successful Bearer upgrade sends `auth_ok`, for protocol 1, since the client offered no
      `protocols` list.
    - A client that wants v2 sends `auth` instead, and the protocol guide says so.
    - A failed Bearer upgrade keeps today's `auth_error`, and is logged by D1.
  - **Test.** A Bearer upgrade receives `auth_ok` with no `protocol` field, and its first request
    succeeds.
- **D10 — the F-T3 residual: `session.cancel` stays on the read loop, but cannot hold it.**
  - **The defect.** `session.cancel` is dispatched inline on purpose (0137 F4), so that it can
    reach work queued on the async workers. It calls `Manager.Cancel` under the read loop's own
    context, with no deadline. A provider whose `Cancel` blocks stalls every frame behind it.
  - **The fix.** It stays inline, but the call runs in its own goroutine, and the read loop
    waits at most 10 s. On timeout it writes `session_cancel_timeout`, logs at `warn`, and moves
    on. The call finishes, or fails, on its own.
  - **Test.** With a provider whose `Cancel` blocks, the cancel's error frame arrives within the
    bound, and a following `ping` is answered.

### Session and event layer

- **D11 — F-S1, High: within a session, live delivery order is seq order.**
  - **The defect.** The pump stamps a seq under `m.mu`, unlocks, and broadcasts only later. On
    the way, it broadcasts the events that the triggering event caused before the triggering
    event itself: `advertiseCommands`, context-pressure notices and budget notices. `emitEvent`
    from other goroutines can interleave too. A client that treats "seq ≤ max seen" as covered
    drops the lower seq.
  - **The fix.** Each session entry gains a delivery mutex. Stamping a seq and broadcasting it
    happen under it, as one step, in both the pump and `emitEvent`. The pump broadcasts the
    triggering event before anything it caused. The same order holds for the history-persist
    call (D17) and receipts.

    Holding the mutex across `onEvent` is safe, because `BroadcastEvent` only enqueues. It
    never blocks: `writeBytes` drops a client whose queue is full, rather than waiting.
  - **Test.**
    - The first events of a new session arrive in seq order.
    - Concurrent `emitEvent` calls and pump events for one session arrive in strictly
      increasing seq.
    - The tests run with `-race`.
- **D12 — F-S2, High, narrowed: replay suppression is visible.**
  - **The defect.** Events marked `Replay` are kept out of live broadcast with no trace. The
    trigger 0073 named, acphttp's `loading` flag, went with Goose. acpagent clears its flag
    unconditionally, and 0199 added `_meta.isReplay` handling. A future provider bug of the same
    shape would still be silent.
  - **The fix.** The manager counts suppressed replay events per session, and logs the count at
    `debug` when a replay burst ends: that is, at the first non-replay event, or at `Create`'s
    return. A replay-marked event that arrives while that session has a turn in flight is
    logged at `warn`, once per turn, with its type. That combination is the signature of a stuck
    flag.
  - **Test.** A replay burst logs one `debug` line with its count. A replay-marked event during a
    turn logs one `warn`.
- **D13 — F-S3, High: two devices claiming one session cannot both win.**
  - **The defect.** `Authorize` checks for an empty owner, unlocks, persists the claim, re-locks,
    and stamps the owner only if it is still empty. But it returns `nil` either way. Two
    concurrent claimers are both told they own the session. The disk keeps the last writer, and
    memory keeps the first.
  - **The fix.** A per-session claim mutex is held across check, persist and stamp, and is taken
    before `m.mu`. The second claimer waits, sees an owner, and gets `ErrForbidden`, as any
    non-owner does. Disk and memory agree.
  - **Test.** Two concurrent claimers: exactly one succeeds, and the persisted owner equals the
    in-memory one. The test runs 200 rounds under `-race`.
- **D14 — F-S4, Med-High: seq never goes backwards after a failed save.**
  - **The defect.** When the close-time `SaveHistory` fails, it is only logged. The next `Create`
    seeds seq from the shorter history on disk, so seqs that were already sent are issued again,
    and clients drop the new events as duplicates.
  - **The fix.** `Meta` and the stored `Record` gain `LastSeq`. The close path sets it to the
    entry's seq and persists `Meta` whether the history save succeeded or not. `Create` seeds seq
    from the higher of the history's last seq and `LastSeq`.
  - **Test.** With `SaveHistory` failing at close, a re-created session's first event has a seq
    above every seq sent before.
- **D15 — F-S5, Medium: a history flush cannot overwrite a newer transcript, or recreate a
  deleted one.**
  - **The defect.** `FlushHistory` takes a snapshot under a read lock and writes it later, with
    no per-session serialization and no purge check. A stale snapshot can overwrite the
    close-time transcript, and a flush after `Delete` recreates the directory through `MkdirAll`.
  - **The fix.**
    - Every history write, from flush, from close, and from D16's reset, goes through one helper
      under a history-write mutex.
    - The helper skips a purged session. It also skips a snapshot whose generation is not newer
      than the last one written: the entry's generation counter goes up on every change to the
      ring.
    - `Delete` removes the history under the same mutex.
  - **Test.**
    - A flush racing a close never leaves the older transcript on disk.
    - A flush after `Delete` leaves no directory.
    - Both are driven by hooks that order the race deterministically.
- **D16 — F-S6, Medium: waiting for a session's create lock ends with the caller's context.**
  - **The defect.** `lockCreate` is a plain mutex, held across the provider's `Start`. One hung
    start wedges every later create or relaunch for that id, with no timeout.
  - **The fix.** The lock becomes a one-slot channel, and `lockCreate(ctx, id)` returns the
    context's error when the caller's deadline passes first. The holder's `Start` keeps its own
    context-bounded deadline.
  - **Test.** While one create holds the lock, a second create with a 100 ms deadline returns
    `context.DeadlineExceeded`, and does not wait.
- **D17 — F-S7, Medium: `/clear` also clears the durable transcript.**
  - **The defect.** `/clear` relaunches through `Create`, which reloads the old durable history.
    A cold reload then replays the conversation the user cleared. `relaunch`'s doc comment says
    the history ring is reset, which is false.
  - **The fix.**
    - `cmdReset` relaunches with an internal reset flag. With it, `Create` seeds an empty ring,
      and writes the empty transcript through D15's helper.
    - Seq continues from `LastSeq` (D14), so clients see no regression.
    - The model-switch relaunch keeps its history, as it says it does.
    - `relaunch`'s doc comment is corrected.
  - **Test.** After `/clear`, a cold `session.history` returns only events from after the reset,
    and the first of them has a seq above the last one before it.
- **D18 — F-S8(a), Low-Med: the pump never writes history itself.**
  - **The defect.** `scheduleHistoryPersist` calls `FlushHistory` inline, on the pump goroutine,
    once a session has been dirty for 5 s. That is an fsync'd write of every dirty session,
    before the event is broadcast.
  - **The fix.** Past the latency bound it fires the history timer at once (`Reset(0)`), so the
    flush runs on the timer's goroutine. The 5 s crash-loss bound is unchanged.
  - **Test.** A store whose `SaveHistory` blocks does not delay the pump's next broadcast.
- **D19 — F-S8(b), Low-Med: a notice for a session that is not live is sequenced, or not sent.**
  - **The defect.** `emitEvent` for a session not in memory broadcasts the event with `Seq=0`.
    An example is "Reset failed … this session is closed". The phone treats `seq ≤ 0` as
    unsequenced, so it shows the notice and keeps it nowhere.
  - **The fix.**
    - When the session has a stored record, the event is stamped with the next seq after
      `LastSeq` (D14), appended to the durable history through D15's helper, and broadcast.
    - When there is no record, it is dropped with a `debug` log. No client could own it.
  - **Test.** A notice for a closed session survives a cold history read with a seq above the
    last one. A notice for an unknown id is not broadcast.
- **D20 — F-S8(c), Low-Med: a question left open at turn end is closed on the phone.**
  - **The defect.** At `turn_complete` or `error`, the pump clears its pending permissions and
    questions without emitting `permission_resolved` or `question_resolved`. The phone also
    clears its own on `turn_complete`. A second device, or a cold history replay, keeps a stale
    card.
  - **The fix.** For each ask still pending at that moment, the pump emits a resolved event with
    status `cancelled` (`event.PermissionStatusCancelled`), sequenced as in D11. Asks that the
    provider already resolved are gone from the map by then, so nothing is resolved twice.
  - **Test.** A permission left open when the turn ends yields exactly one `permission_resolved`,
    `cancelled`. One resolved during the turn yields none extra.
- **D21 — F-S8(d), Low-Med: quota detection matches whole words and specific phrases.**
  - **The defect.** `agenterr` matches raw substrings. In the list that settles messages matching
    both kinds, `hardQuotaWords`:
    - `plan` matches "explanation";
    - `spend` matches "suspend";
    - `credit` matches "accredited".

    `limit reached` would class a context-length "limit reached" as a hard quota block.
  - **The fix.**
    - The single words in `hardQuotaWords` (`billing`, `credit`, `quota`, `spend`, `plan`,
      `weekly`, `monthly`) match on word boundaries.
    - `limit reached` no longer counts as quota when the text names a context, token or length
      limit.
    - The phrases are unchanged otherwise.
  - **Test.**
    - Every message in the existing corpus keeps its classification.
    - New negative cases keep their non-quota class: "explanation", "suspended", "accredited",
      "context length limit reached", "maximum token limit reached".

### CLI, configuration, authentication store and update

- **D22 — §6.2, High (and Windows): `setup-service --no-start` never stops or starts the
  service.**
  - **The defect.**
    - **macOS:** `setupLaunchdAgent` calls `stopAndWaitDarwin` unconditionally, and gates only
      the bootstrap on `!NoStart`. So `--no-start` stops a running service and leaves it
      stopped.
    - **Windows:** `setupSchtasks` ignores the flag, and starts the task.
  - **The fix.** With `--no-start`, every platform writes the definition and touches no running
    instance:
    - macOS skips the teardown and the bootstrap. When a job is running, it prints that the job
      keeps its old definition until restarted.
    - Windows skips `startWindows`.
    - Linux is unchanged.
  - **Test.** Fake `launchctl` and `schtasks` runners record no stop or start call under
    `--no-start`, and the definition is written.
- **D23 — §6.4, Medium: every configuration key can be set from the environment.**
  - **The defect.** `setDefaults` (`internal/config/load.go`) has no default for
    `limits.ws_read_deadline_seconds`, `limits.ws_resume_window_seconds`, `limits.tcp_keepalive.*`
    or `providers.codex.sandbox_broken_policy`. `AutomaticEnv` resolves only known keys, so
    their `MCREMOTE_*` variables are ignored.
  - **The fix.** Each gets a `SetDefault`. A reflection test then walks every `mapstructure`
    leaf of `Config`, and fails when a key has no default. An allowlist names any key that is
    deliberately unbound, each with its reason. This carries out
    [0090-PLAN-config-template-completeness.md](0090-PLAN-config-template-completeness.md)
    Phase 3 Task 3.1, which 0090 has not run, and that PLAN gets a note.
  - **Test.** Each newly bound key is overridden by its environment variable. The reflection
    test fails on a scratch copy with one `SetDefault` removed.
- **D24 — §6.5, Medium: `--data-dir` is resolved in one place.**
  - **The defect.** `serve` and `paths` assign the raw flag value after `config.Load`, which had
    already bound it and made it absolute. A relative `--data-dir` then fails in
    `appdirs.WithDataDir` ("must be absolute"). `pair` re-reads a parent command's flag the same
    way.
  - **The fix.** `serve` and `paths` drop their re-assignment, because `Load` already applied the
    flag. `pair` makes its parent's value absolute against the working directory, through an
    exported `config.AbsDataDir`, before `RecomputePaths`.
  - **Test.** `mcremote paths --data-dir rel/dir` reports `<cwd>/rel/dir`. `serve`'s flag path is
    tested the same way.
- **D25 — §6.7, Medium: `devices.json` is created under the cross-process lock.**
  - **The defect.** `OpenStore` stats the file and creates an empty one outside `withPathLock`.
    A concurrent `pair create` can be truncated away.
  - **The fix.** The stat and the create run inside `withPathLock`, and the file is created only
    if it is still absent there.
  - **Test.** `OpenStore` racing a writer that adds a device never loses the device. The test
    runs 200 rounds.
- **D26 — §6.6, Medium: `mcremote update` says when the service runs a different binary, through
  `go-selfupdate-lib`.**
  - **The defect.** The update replaces the binary it runs from. When the service runs a
    different binary, the refresh child's `binaryWarning` puts that in its result. `mcplib`
    v1.4.1 drops the `Detail` that carries it, so the update reports success while the service
    keeps the old build.
  - **Not patched in `mcplib`.** Magic is to drop `mcplib` for `go-selfupdate-lib`
    ([0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
    D4). A workaround in `internal/updateclient.Reconciler`, which that migration deletes, would
    be thrown away.
  - **Adopting the library fixes it, with no new Magic code.** Every link of the chain already
    exists:
    - Magic's refresh receipt already lists the mismatch: `RefreshResult.Warnings`, JSON
      `warnings`, is appended from `binaryWarning` (`internal/cli/service/refresh.go`).
    - `go-selfupdate-lib` v1.15.2's `service.NewExecReconciler` reads the child receipt's
      `warnings` into `ReconcileResult.Warnings`.
    - The update run adds each one to `Result.Warnings`, as an `EventWarning`.
    - The library's command prints each on standard error, as `warning: <text>`
      (`selfupdate/cli/run.go`).
  - **The decision.** §6.6 is fixed by 0054's adoption (S1 and S3), and 0201 adds the regression
    test that proves it end to end.
  - **Test.**
    - A refresh whose definition names another binary yields a receipt with that warning.
    - An update driven by `selfupdatetest`, through the library's command, prints
      `warning: the definition runs …` on standard error, and still exits 0.
    - It is seen failing on today's `mcplib` path, which prints nothing.
- **D27 — §6.9, Medium: the service never puts its runtime directory under a private `/tmp`.**
  - **The defect.** The systemd units set `PrivateTmp=true`. `XDG_RUNTIME_DIR` is set only when
    the installer's environment had it. Without it, and without `/run/user/$UID`, appdirs falls
    back to `os.TempDir()`, which for the service is its private `/tmp`. The admin socket then
    splits from the CLI's.
  - **The fix.** When the installer has no `XDG_RUNTIME_DIR`, both unit templates (mcremote and
    mcrelay) emit `Environment=XDG_RUNTIME_DIR=%t`. That is the systemd specifier for the user
    manager's runtime directory (systemd.unit(5), "Specifiers").
  - **Test.** A rendered unit with an empty installer value contains
    `Environment=XDG_RUNTIME_DIR=%t`. One with a value keeps it.
- **D28 — §6.10(a), Low: a panicking catalog fetch cannot wedge the picker cache.**
  - **The defect.** `picker/cache.go` clears its in-flight entry and closes `done` only after a
    normal return. A panicking fetch leaves every later caller waiting until its own context
    ends.
  - **The fix.** The cleanup runs in a `defer`, and a panic becomes an error for every waiter.
  - **Test.** A panicking fetch returns an error to its caller and its waiters, and the next
    fetch runs.
- **D29 — §6.10(b), Low: a pair-code store that cannot write is not hidden.**
  - **The defect.** `PairCodeStore.Take` (`internal/auth/paircode.go`) ignores two `s.save`
    errors (`_ = s.save(data)`). Neither is the burn of a valid code. That save is checked,
    and fails the claim.
    - On an unknown code, one ignored save persists the purge of expired codes.
    - On an expired code, the other persists its removal.

    A store that cannot write stays invisible, and the expired entries come back on the next
    load.
  - **The fix.** Both failures are logged at `warn`, with the store path and the error. The
    claim's result is unchanged: `ErrInvalidPairCode` and `ErrExpiredPairCode` are still
    returned, because the code was not valid either way.
  - **Test.** A store whose save fails logs one `warn` on each path, and returns the same error
    as today.
- **D30 — §6.10(d), Low: a service installed under a custom unit name is controlled by that
  name.**
  - **The defect.** `setup-service` honours `--unit-name`, but `control.go` (stop, start,
    status, installed) and the refresh path derive the unit from the product name. A custom-named
    service cannot be stopped, updated or reported on.
  - **The fix.** `setup-service` records the unit name it used in `<state_dir>/service-unit`.
    Every control and refresh path resolves the name through one function, which returns the
    recorded name, or the product name when there is none. Removing the service removes the
    record.
  - **Lands after 0054's S1.** S1 moves service control to `go-selfupdate-lib`'s
    `systemd`/`launchd`/`taskscheduler` backends, each built with an `Options{Name: …}`. The
    resolver feeds that name, so it is written once, against the backends Magic will keep. It is
    not written against `control.go`, which S1 replaces.
  - **Test.** With a recorded custom name, every backend is built with that name. With none, it
    gets the product name.
- **D31 — §6.10(e), Low: `smoke-protocol` speaks v2, and its dead code is gone.**
  - **The defect.** `scripts/smoke-protocol/main.go` offers no `protocols`, so it only ever
    exercises v1, and has an empty `if waitAfter > 0 {}` block.
  - **The fix.** It offers `[2, 1]`, asserts that v2 was negotiated, and the empty block is
    deleted.
  - **Test.** The smoke tool is built and vetted. Its run against a local daemon reports v2.
- **D32 — §6.11, unranked: `doctor` checks each engine.**
  - **The defect.** `doctor` reports credential files, but not whether each enabled engine's
    binary is present and ready.
  - **The fix.** For each enabled provider, `doctor` reports the resolved binary path, or
    "missing", and whether the provider reports itself ready (`Ready()`). This is a lookup
    only, with no engine started and no tokens spent.
  - **Test.** With a fake registry holding one ready provider and one missing, the report lists
    both correctly.

### Phone app (Flutter)

- **D33 — §7.1, High: every transcript cache write is serialized, however many instances
  exist.**
  - **The defect.** The settings screen builds its own `TranscriptCache` instances, for usage and
    for clear. Their writes bypass the live notifier's `_serial` chain, which belongs to one
    instance.
  - **The fix.** The chain becomes process-wide (a static field), so every instance's operations
    on the cache directory are serialized.
  - **Test.** Two instances interleaving a save and a `clear` leave a consistent index. The test
    is seen failing with the per-instance chain.
- **D34 — §7.2, Medium: editing Host updates the pair hints on screen at once.**
  - **The fix.** `_clearPendingPairHints()` on host edit runs inside `setState`.
  - **Test.** A widget test: after editing the host, the stale hint is gone without waiting for
    the probe debounce.
- **D35 — §7.3, Medium: a double tap cannot claim a pair code twice.**
  - **The defect.** `_busy` is released right after the clipboard read. `_applyPair` then awaits
    probes unguarded, and a second tap starts a second claim of a one-shot code.
  - **The fix.** `_busy` covers paste, probe and claim, and is released in one `finally`.
  - **Test.** A widget test with two taps in quick succession: one claim is issued.
- **D36 — §7.4, Medium: a finished reply is not re-rendered as streaming.**
  - **The defect.** The post-frame callback in `chat_bubble.dart` checks `mounted` and
    `_upToDate(true)`, but not `widget.streaming`. It can render a finished reply in streaming
    mode.
  - **The fix.** It checks `widget.streaming` too, as the tier-timer path already does.
  - **Test.** A widget test that finishes a reply between frame and callback renders it final.
- **D37 — §7.5(a), Low: a failure while clearing credentials is shown.**
  - **The fix.** `clearSecrets()` in the settings screen is caught. On
    `SecureStorageUnavailable` the user sees an error, instead of a tap that does nothing.
  - **Test.** A widget test with a throwing store shows the error.
- **D38 — §7.5(b), Low: `NotificationService.init` runs once, however many callers race it.**
  - **The fix.** `init` keeps its in-flight `Future`, and concurrent callers await the same one.
    `_ready` is still set at the end.
  - **Test.** Two concurrent `init` calls initialise the plugin once.
- **D39 — §7.5(c), Low: a failed download removes its partial file and reports the original
  error.**
  - **The defect.** In `app_update.dart`, `sink.close()` inside the `catch` can throw before the
    partial file is deleted. That masks the original error, and leaves a partial APK.
  - **The fix.** The close is guarded, the partial file is deleted in a `finally`-style step, and
    the original error is rethrown.
  - **Test.** A download whose stream fails and whose `close` also throws deletes the file, and
    reports the stream's error.

### Consequences

- Good, because every finding the triage confirmed is fixed by a change that has a failing-first
  test, rather than being re-recorded.
- Good, because the High findings change user-visible behaviour:
  - order of delivery (D11);
  - ownership (D13);
  - silent drops (D3);
  - a stopped service (D22);
  - cache integrity (D33).
- Good, because three of the fixes are guards that keep their whole class from coming back:
  - the configuration-key reflection test (D23);
  - the single history-write helper (D15);
  - ordered delivery (D11).
- Neutral, because D9 changes the documented protocol only by adding a frame that the docs
  implied. D19 changes `Seq=0` notices into sequenced events, which the phone already handles.
- Bad, because D11 and D15 add locks to the event and persistence paths. Their order is fixed
  as follows, documented in the code, and tested under `-race`:
  - delivery mutex, then `m.mu`;
  - claim mutex, then `m.mu`;
  - history-write mutex alone.
- Bad, because 39 decisions make a long PLAN. Its phases are ordered so that each can ship and
  be reverted alone.
- Neutral, because D26 and D30 wait on
  [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md)'s
  S1. Magic is moving from `mcplib` to `go-selfupdate-lib`, so these two are fixed in the
  library-backed code rather than in code that migration deletes. 0054 is still `proposed`, so
  their phase waits for its acceptance and its S1. The other 37 decisions do not depend on it.
- Neutral, because one finding already fixed rests on `mcplib`. That is §6.1: an update on a host
  with no installed service. The move must keep it fixed, and 0201's PLAN adds the check to that
  phase.

### Confirmation

- Every decision's test is seen failing on today's code before its fix, and the PLAN records it.
- The PLAN's planted breaks fail their tests on scratch copies.
- Each phase passes:
  - `make pre-add-check` on its Go files;
  - `go test -race ./...`;
  - `make lint` and `make staticcheck`;
  - `make preflight` for the Flutter phase;
  - `make ci-windows` on the Windows test host.
- D8's ALPN, D22's `--no-start` and D27's unit are also checked on real hosts: the Mac, the Linux
  test host, and the Windows test host.
- The triage report is updated as each finding closes.

## Pros and Cons of the Options

### One MADR, a decision per finding, one phased PLAN

- Good, because the shared mechanisms are decided once: delivery order, history writes, session
  locks.
- Good, because one record maps 1:1 onto the triage report's rows.
- Bad, because it is long, and a reviewer has to read it in sections.

### One record per finding, or per cluster

- Good, because each record is short.
- Bad, because D11, D14, D15, D17 and D19 depend on one another, and splitting them scatters the
  lock-order and seq reasoning across five records.
- Bad, because some 39 records would have to be reviewed.

### Fix the High findings now, accept the rest as risk

- Good, because it is less work now.
- Bad, because the owner asked for every finding to be fixed.

### Document instead of change

- Good, because it is cheap.
- Bad, because most of these are correctness defects, and no amount of documentation fixes a
  race or a lost write.

## More Information

- The evidence for every "still present" is in
  [0073-REPORT-triage-of-the-non-goose-findings.md](../reports/0073-REPORT-triage-of-the-non-goose-findings.md),
  at `56ea5621`.
- **D8's measurement:** the scratch daemon was built from this tree, ran in `selfsigned` mode on
  `127.0.0.1`, and was probed with `openssl s_client -alpn h2,http/1.1`. Its result was "ALPN
  protocol: h2". The daemon and its state lived in the session scratch directory, and were
  removed afterwards.
- **External behaviour relied on:**
  - Go 1.27 `http.Server.Protocols` (`go doc net/http Server.Protocols`);
  - systemd's `%t` specifier (systemd.unit(5));
  - viper's `AutomaticEnv` resolving only known keys (`internal/config/load.go`'s own comment at
    `pair.advertise_host`).
- The PLAN is
  [0201-PLAN-fix-the-open-findings-of-the-0073-debug-pass.md](0201-PLAN-fix-the-open-findings-of-the-0073-debug-pass.md).
