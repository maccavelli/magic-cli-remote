---
status: proposed
date: 2026-10-21
decision-makers: Project Owner
consulted: "go-selfupdate-lib v1.17.1–v1.17.2, acp-go-sdk2 v1.15.0–v1.19.0 and go-llmprovider-sdk v1.21.0, read from full clones at their tags; their GitHub release notes; a scratch copy of this repository at 707d300d with both pins moved; gobble-cli's go.mod on its default branch"
informed: anyone who changes this module's dependencies, its service code or its ACP provider
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Move to the current library releases, and close the adoption gaps they leave open

## Context and Problem Statement

On 2026-10-21 the owner asked for an assessment of the current releases of `go-selfupdate-lib`,
`acp-go-sdk2` and `go-llmprovider-sdk`, for the pins to move to them, and for an analysis of whether
Magic should adopt more of any of them. The aims are better standards and best-practice adherence,
no duplicated code or functionality, and a lean and efficient codebase.

The standing records are:

* [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
  `accepted`, PLAN `complete`: "fullest adoption" of `go-selfupdate-lib` (its O1).
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md),
  `accepted`, PLAN `in-progress`. Its amendment of 2026-10-19 proposes **B3**, a move to `v1.15.2`.
  B3 was never approved or run.
* [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md),
  `accepted`, PLAN `in-progress`, with **P1** (`make check-libs`) and **P2** (full-SemVer pins on
  `golang.org/x/mod/semver`) open.
* [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md),
  `accepted`: no `go-llmprovider-sdk` in `go.mod`. Bringing it back takes a MADR that supersedes it.
* [0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md](../reports/0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md):
  replacing Magic's provider auth with the SDK is not feasible; the route is gobble.
* [0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md](0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md),
  `accepted`: no release signing.

Which releases should Magic pin, which library capabilities should it adopt or stop duplicating,
and which should it decline, with what reason?

### Releases [observed 2026-10-21, `git ls-remote` and the GitHub releases API]

| Library | Magic pins | Latest | Latest's commit | Published |
| :--- | :--- | :--- | :--- | :--- |
| `go-selfupdate-lib` | `v1.17.1` | **`v1.17.2`** | `036289ecc701d84c7749f4950a57a154c1af2cf4` | tag only, 2026-10-21 (the library publishes no GitHub releases) |
| `acp-go-sdk2` | `v1.15.0` | **`v1.19.0`** | `d804fb73b1f5a7bfd1036cccbc2ec6afd194e5aa` | 2026-10-21T16:31:06Z, immutable |
| `go-llmprovider-sdk` | not a dependency | `v1.21.0` | `d902c03007cd1dbe7ef518887da5b1c03155a168` | 2026-10-21T17:34:05Z, not immutable |

`acp-go-sdk2` released `v1.15.1`, `v1.15.2`, `v1.16.0` to `v1.16.3`, `v1.17.0`, `v1.17.1`,
`v1.18.0` and `v1.19.0` after Magic's pin.

### How the surfaces were compared

The exported API of each library at each tag was read from its git tree with a source scanner. Every
non-test `.go` file outside `internal/`, `cmd/`, `example/` and `testdata/` was scanned for top-level
exported funcs, methods on exported types, types, vars and consts. Nothing from the clones was built
or run. The scanner does not see struct fields, so a release's notes were read for those.

### go-selfupdate-lib, `v1.17.1` → `v1.17.2`

* **The API is identical.** The scan finds 14 packages and 495 exported declarations at both tags,
  with 0 added and 0 removed. `git diff --stat v1.17.1 v1.17.2` over every non-test `.go` file and
  both reusable workflows, `build-selfupdate-release.yml` and `publish-selfupdate-release.yml`, is
  empty.
* **What changed:** `go.mod` moves to `go 1.26.10` and `toolchain go1.27.3`, for GO-2026-6809 in
  `crypto/x509` (its CHANGELOG, "Security"). CI skips Markdown-only pushes.
* **It fixes a gap left open here.** The publish workflow sets up Go from the library's own
  `go.mod` (`go-version-file: .core-lib-release-tools/go.mod`) to run its release tools. At
  `v1.17.1` that is `toolchain go1.27.2`; at `v1.17.2`, `go1.27.3`. The CHANGELOG says so: "For a
  caller that pins `v1.17.2`, the publish workflow builds its release tools with Go 1.27.3."
* **What Magic imports:** 10 of the 14 packages. These are `selfupdate`, `selfupdate/cli`,
  `selfupdate/service` with `systemd`, `launchd` and `taskscheduler`, `codesign`, `releasespec`,
  `selfupdatetest` and `buildinfo` (`git grep` of every import).
* **The four not imported, each with a reason:**

  | Package | Why Magic does not use it |
  | :--- | :--- |
  | `selfupdate/archive` | Magic ships raw binaries, not archives (its release spec). |
  | `selfupdate/service/scm` | Windows service control; Magic runs a per-user Task Scheduler task without elevation ([0116](0116-MADR-windows-and-linux-arm64-build-targets.md) D12). |
  | `selfupdate/verify/signednote` | no release signing ([0205](0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md)). |
  | `selfupdate/verify/ghattest` | needs a logged-in `gh` on every updating host ([0054](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md) "What stays deliberately unadopted"; 0205 G1). |

### acp-go-sdk2, `v1.15.0` → `v1.19.0`

* **The API grows, compatibly.** The scan finds 16 additions and 0 removals. Each release's own
  `make api` gate reports no incompatible change (its release notes).
  * **`Err()` and `Wait()`** on `Connection`, `AgentSideConnection` and `ClientSideConnection`, and
    the exported `ErrNotificationQueueOverflow` (`v1.18.0`).
  * **A new package, `agentproc`** (`v1.19.0`): `Command{Cmd, Grace, ConnectionOptions}` with
    `Start(client)`, and `Process` with `Conn`, `Done`, `Wait` and `Close`; `ErrTerminated` and
    `ErrKilled`.
* **The ACP schema is unchanged,** `1.25.0`, in every release (each release's notes).
* **Requirements:** Go 1.26.10 or newer, and `golang.org/x/sys v0.49.0`. Magic is on `go 1.27.3`
  and already requires `golang.org/x/sys v0.49.0` (`go.mod` line 20).
* **Behaviour changes, against Magic's use:**

  | Release | Change | Effect on Magic |
  | :--- | :--- | :--- |
  | `v1.15.2` | `Prompt` no longer sends `session/cancel` for a prompt it never wrote (issue #7) | Magic's mitigation, `submitPrompt`'s `ctx.Err()` check (`internal/provider/acpagent/session.go:597-613`), can go: B3 (proven below). |
  | `v1.15.2` | requests received before the input's end are answered; subagent updates gated on `subagents` | none: Magic advertises no `subagents`, and subagent sessions are deferred ([0195](0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md)). |
  | `v1.16.0` | fewer allocations; the wire bytes are unchanged (a 1,248-instance golden test) | faster encode and decode on every ACP turn; nothing to change. |
  | `v1.16.1` | `Close` ends notification handlers' contexts at once | consistent with Magic's teardown; nothing to change. |
  | `v1.17.0` | a nil constructor argument panics; peer-fault lines log at Warn, the connection's end at Debug; a handler's error text reaches the peer | Magic passes no nil; no Magic test asserts those log levels (`git grep` of the ACP tests); Magic's client handlers return errors the agent may see, as before. |
  | `v1.18.0` | a notification's error matches `io.EOF` after the peer's end | Magic's checks use `errors.Is(err, acp.ErrPeerDisconnected)` and `ErrConnectionClosed` (`session.go:509`), which still match. |

* **`agentproc` fits Magic's launch path, but for one gap.** Magic starts an agent in
  `spawnAgent` (`internal/provider/acpagent/acpagent.go:455`):
  * `launch.Command`, which on Windows quotes an npm batch shim's command line into
    `SysProcAttr.CmdLine` ([0159](0159-MADR-the-windows-service-path-is-half-wired.md) D23, D24);
  * a stderr writer that scrapes quota lines;
  * `procutil.SuperviseStarted`, a job object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` on Windows
    (`acpagent.go:512`; `internal/procutil/procutil_windows.go:261-271`;
    [0150](0150-MADR-the-windows-tree-kill-was-never-wired-up.md));
  * `procutil.KillProcessGroup` on teardown;
  * and `acp.NewClientSideConnection(s, stdin, s.wire.TeeReader(stdout), …)` (`acpagent.go:531`),
    whose `TeeReader` is `wirecap`'s raw-frame capture
    ([0137](0137-MADR-prompt-to-first-token-latency-regression.md) Phase 1).

  `agentproc` at `v1.19.0`:
  * **Keeps the caller's `Cmd`.** Its `Path`, `Args`, `Env`, `Dir` and `Stderr` stay as set. On
    Windows its `prepare` sets nothing, so `launch.Command`'s `CmdLine` survives, and it assigns
    the agent to a kill-on-close job after start, as `SuperviseStarted` does. On Unix it sets
    `Setpgid`, which Magic's `procutil` also sets, and refuses only a preset `Pgid`.
  * **Owns both pipes, and hands the stdout pipe straight to `NewClientSideConnection`.** There is
    no hook for a reader, so `wirecap`'s tee would be lost (`agentproc/agentproc.go`, `Start`).
  * **Stops in bounded steps:** it closes the agent's input, waits up to the grace, sends SIGTERM to
    the group on Unix, then kills the group or ends the job. It also ties the process to the
    connection: either ending stops the other. Magic does this by hand today, with `cmd.Wait`, its
    exit watcher, `watchConnClose` (`session.go:1244`) and `KillProcessGroup`.
* **The one-pin rule with gobble already does not hold.** 0184 D12 has Magic and gobble pin the same
  tag. gobble-cli's `go.mod` pins `acp-go-sdk2 v1.16.3` (and `go-selfupdate-lib v1.17.0`,
  `go-llmprovider-sdk v1.17.0`); Magic pins `v1.15.0`. gobble's move is gobble's own (0184 F4, G5).

### go-llmprovider-sdk `v1.21.0`, against what Magic does

The SDK is a client for LLM vendor APIs. It has 15 packages and 411 exported declarations:
generation and streaming (`Provider`, `Stream`, `GenerateText`), tool calls, retries
(`WithRetry`, `WithStreamRetry`), token sources (`auth`), `catalog`, nine vendor providers, a test
kit and a `wizard`. Magic calls no vendor API; it drives agent CLIs and engines. Each overlap a
reader might suspect:

| SDK capability | Magic's counterpart | Duplication? |
| :--- | :--- | :--- |
| `catalog.List`, `Recommended`, `Usable` | `internal/picker`, fed by the engines | No: engine-backed by decision ([0185](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md); the risks in [0183](../reports/0183-REPORT-go-llmprovider-sdk-catalog-parity-assessment.md)). |
| `auth` token sources and logins | `internal/providerauth`, `credstore` | No: Magic writes each agent's own store and holds no vendor token (0202). |
| `ClassifyHTTPError`, `ClassifyStreamFailure` | `internal/agenterr.Classify` | No: the SDK classifies an `*http.Response` from a vendor; `agenterr` classifies the free text an agent CLI forwards (`agenterr.go:1-8`). Different inputs. |
| `WithRetry` | engine reconnect backoffs | No: Magic retries a process, not a request. |
| `Usage`, cost | session cost totals ([0189](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md), [0199](0199-MADR-total-grok-per-turn-costs-into-the-session-cost.md)) | No: Magic adds up what the agent reports; it computes no price. |

Nothing Magic implements is something the SDK would replace. Adding it would add a dependency to
remove no code. gobble, which Magic runs over ACP as a separate process, already pins the SDK; that
remains the route for any SDK-backed capability (0202, option C).

### The pin moves, proven on a scratch copy [2026-10-21]

On a copy of the tracked tree at `707d300d`, with a scratch build cache, nothing in the tree written
(`r0207/pin_probe.py`):

```text
go: upgraded github.com/maccavelli/go-selfupdate-lib v1.17.1 => v1.17.2
go: upgraded github.com/maccavelli/acp-go-sdk2 v1.15.0 => v1.19.0
go mod tidy: exit 0
go.mod | 4 ++--     go.sum | 8 ++++----
go build ./...: exit 0
go vet darwin/arm64, windows/amd64, linux/amd64: exit 0
go test -race ./...: 41 ok; one FAIL: TestWorkflowPinsMatchGoMod
B3 premise on v1.19.0, mitigation removed:  --- PASS: TestPromptSendsNothingWithAnEndedContext
B3 premise on v1.15.0, mitigation removed:  lifecycle_test.go:77: a prompt with an ended context put "session/cancel" on the wire
```

* **`go.mod` changes two lines,** the two requirements, and `go.sum` four. Tidy adds nothing else.
* **The one failure is the pin guard doing its job.** The copy moved `go.mod`'s `go-selfupdate-lib`
  but not `ci.yml`'s two workflow pins, which `TestWorkflowPinsMatchGoMod` requires to match. The
  real change moves the three together (0054 D1).
* **B3's premise holds on `v1.19.0`:** without the mitigation, the test passes there and fails on
  `v1.15.0`.

### Gaps found in Magic

* **F1 — An update an agent starts inside the service stops the service under itself.** Inferred
  from the code, not reproduced.
  * 0054 left the library's `service.Detacher` handoff unadopted, because "updates run from an
    operator shell, never inside the daemon, so nothing would call it" ("What stays deliberately
    unadopted").
  * The premise does not hold. An agent session the daemon runs executes shell commands for the
    phone's user, so `mcremote update` can run as the daemon's descendant.
  * **On Linux** the unit has `KillMode=control-group` (`mcremote.user.service.tmpl:44`). The
    update's own `Stop` would then kill every process in the unit's cgroup, the update included,
    before the swap and the restart. The service would be left stopped.
  * **On Windows** the agent's tree is in a job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, whose
    handle the daemon holds (`procutil_windows.go:261-271`). When the update's `Stop` ends the
    daemon, the tree, the update included, would die.
  * **On macOS** the agent runs in a process group of its own (`Setpgid`). launchd reaps only the
    job's group, so the update would survive.
  * **The library covers it.** `cli.Options.HandOff` (`selfupdate/cli/run.go:57-68`) with
    `service.HandOffFunc` detaches a run that `Inside` reports inside the service. `systemd.Unit.Inside`
    checks the cgroup (`systemd/detach.go:25-50`), `taskscheduler.Task.Inside` walks up the parents
    (`taskscheduler/detach.go:12-24`), and `launchd.Job.Inside` checks the group and ancestors.
    Magic sets no `HandOff` (`internal/updateclient/client.go:250`, `cli.Run` with no hand-off).
  * **On Windows the detached run breaks away from its job only when the job allows it.** The
    library adds `CREATE_BREAKAWAY_FROM_JOB` conditionally (`internal/winproc`, `detachedAttr`), and
    Magic's agent job sets no `JOB_OBJECT_LIMIT_BREAKAWAY_OK`. A handoff there may also need Magic's
    job to allow breakaway; that is to be measured, not assumed.
* **F2 — `setup-service` still stops and starts services with its own code.** 0054 O1 moved "every
  concern the library owns"; D3 replaced only `control_schtasks.go`'s update-path functions. Left:
  * `stopAndWaitDarwin` (`launchd_wait.go:64-93`), against `launchd.Job.Stop`, which "boots the job
    out … and waits until launchd has let it go";
  * `launchdJobRunning` (`launchd_wait.go:103-112`), against `launchd.Job.Running`;
  * `startWindows` and `stopWindows` (`control_schtasks.go:60-104`), against
    `taskscheduler.Task.Start` and `Stop`.
  * **The difference that matters:** `stopWindows` is "a TerminateProcess, NOT a graceful signal …
    the daemon gets no drain" (its comment). `Task.Stop` asks the daemon to shut down first, the
    drain PLAN 0054 S9d and S10b proved (exit 0, against 1067 for a forced end). So
    `setup-service --remove` and `--force` kill the daemon on Windows, where `update` drains it.
  * **Not all of it is duplicated.** Setup also enables a unit persistently
    (`systemctl --user enable`, `setup.go:480`), which no backend's `Start` does, and runs on a
    first install, when nothing is loaded yet. The share that can move is measured in the PLAN.
* **F3 — Two SemVer parsers, and the dependency that replaces them, unused.**
  `provider.ParseSemver` and `SameVersion` (`internal/provider/version.go:18-59`) and opencode's
  `parseSemver` and `CompareVersions` (`internal/provider/opencode/version.go:44-104`) are near
  copies. They differ only in whether a space ends a version. `golang.org/x/mod` `v0.41.0` is a
  direct requirement. This is [0186](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md)
  D8 and its PLAN's P2, accepted and not done.
* **F4 — 0186's PLAN P1, `make check-libs`, has not run.** There is no `check_libs.json` in the
  repository.
* **F5 — B3 is stale.** It targets `v1.15.2` and its first step stops on "a later tag". `v1.19.0`
  supersedes it, and the scratch run above proves its premise there.
* **F6 — Minor.**
  * `binaryWarning` compares paths as strings (`refresh.go:600`, `exe == unitBinary`), so two
    spellings of one Windows path warn. `service.SameExecutable` compares cleaned paths
    case-insensitively on Windows, then by `os.SameFile`.
  * `watchConnClose`'s comment (`session.go:1234-1243`) says an inbound queue overflow tears the
    connection down. Magic runs `OverflowDropNewest` (`acpagent.go:1049`;
    [0167](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md)
    D17), under which it cannot. Its message, "agent connection lost", could say why: `conn.Err()`
    tells the peer's end of output (`io.EOF`) from a read error.
  * Kept as is, and not a finding: Magic's `samePath` (`internal/cli/service/migrate.go:53`) is not
    `SameExecutable`'s duplicate. It applies Windows rules on any host, which the Windows migration
    tests on macOS and Linux need; `SameExecutable` applies them only on Windows.

## Decision Drivers

* Pin released tags only, each with its proof.
* Delete a mitigation when its fix ships, and only then.
* One library per change, so a regression bisects to one library (0186).
* Adopt a library capability where Magic duplicates it, unless the library's version loses something
  Magic relies on.
* Decline a dependency that removes no code.
* Correct a recorded fact that no longer holds, in the record that states it.
* Quota-spending verification waits for the owner (0184 D12; the provider-quota rule).

## Considered Options

* **A** — Move both pins now; adopt the handoff and the backends' stop and start; finish 0186's
  P1 and P2; request an output hook for `agentproc`; keep `go-llmprovider-sdk` out of scope
* **B** — Move the pins only, and record the gaps for later
* **C** — As A, and adopt `agentproc` now, dropping `wirecap`'s ACP capture
* **D** — As A, and adopt `go-llmprovider-sdk`, superseding 0185

## Decision Outcome

Proposed: "A", because it takes both current releases, each proven on a scratch copy. It also closes
the one gap with a correctness cost, F1, and the one with an operator-visible cost, F2's hard kill
on Windows. It finishes two accepted pieces of work that remove duplicated code (F3, F4). And it
adopts nothing that would cost Magic something it relies on, which rules out C and D.

### The decisions (proposed)

* **D1 — Pin `go-selfupdate-lib v1.17.2`.** `go.mod` and both `ci.yml` pins move together, the pins
  to `036289ecc701d84c7749f4950a57a154c1af2cf4` with the comment `# go-selfupdate-lib v1.17.2`
  (0054 D1; `TestWorkflowPinsMatchGoMod`). No Go code changes.
* **D2 — Pin `acp-go-sdk2 v1.19.0`, replacing 0184's B3.** It is a 0184 B phase, as B1 and B2 were,
  with B3's steps on the new tag:
  * remove `submitPrompt`'s mitigation, keeping `TestPromptSendsNothingWithAnEndedContext`;
  * update 0184's status note: issue #7 is closed.
  * **Done only when `make live-grok` passes,** which spends quota and waits for the owner (0184
    D12, G6).
  * gobble's pin stays gobble's.
* **D3 — Adopt the update handoff (F1), after reproducing it.** First reproduce it on the Linux test
  host, with a scratch unit whose service runs `mcremote update` as a descendant. Then wire
  `cli.Options.HandOff` through `service.HandOffFunc` and `service.ReportFunc` for the backend the
  update already builds, and the program-start hop the library documents (`LoadHandOffEnv`,
  `HandOffHop`).
  * On Windows, measure whether the detached run leaves Magic's agent job. If it does not, allowing
    breakaway (`JOB_OBJECT_LIMIT_BREAKAWAY_OK`) on that job is part of this decision.
  * This **amends 0054's "What stays deliberately unadopted"**: its premise, that no update starts
    inside the service, does not hold.
* **D4 — `setup-service` stops and starts through the library backends where their semantics match
  (F2).**
  * **Stop,** for `--remove`, `--force` and the before-rewrite stop, goes through the backend's
    `Stop`. On Windows that gives setup the drain `update` already has; on macOS, the library's
    bootout-and-wait.
  * **Start** goes through `Start` where it matches. Setup keeps persistent enabling and the
    first-install path, which no backend owns.
  * `stopAndWaitDarwin`, `launchdJobRunning`, `startWindows` and `stopWindows` go when nothing
    calls them.
  * The PLAN measures each call site first.
* **D5 — Finish 0186's P2 and P1 (F3, F4).** They are accepted already, and this record re-decides
  neither; it puts them in the same PLAN, since F3 is the duplication the owner asked about.
* **D6 — `agentproc` is not adopted yet; the SDK is asked for an output hook (C, declined).**
  * Ask `acp-go-sdk2` for a way to wrap the stdout reader `Start` hands the connection, such as a
    `Command.Output func(io.Reader) io.Reader`.
  * Filed at the owner's ask on 2026-10-21 as `maccavelli/acp-go-sdk2#10`, "agentproc: no hook on
    the agent's output stream, so a client that records the raw wire cannot adopt it (v1.19.0)".
    It proposes a reader wrapper or a write-only tap, either opt-in. It also notes two related
    points it does not request: ending an unused agent at once, and Windows job breakaway (F1).
  * Once a release has it, adopting `agentproc` in `spawnAgent` replaces `SuperviseStarted`,
    `KillProcessGroup` and the hand-written process-to-connection tie for ACP agents. That adoption
    takes an amendment here.
* **D7 — `go-llmprovider-sdk` stays out of scope (D, declined).** 0185 stands, re-checked against
  `v1.21.0`: nothing Magic implements is something the SDK would replace. gobble remains the
  route.
* **D8 — The minor items (F6).**
  * `binaryWarning` uses `service.SameExecutable`.
  * `watchConnClose`'s comment is corrected, and its message says why the connection ended, from
    `conn.Err()`.
  * `samePath` stays.
* **D9 — The unused library packages stay unused,** for the reasons in the table above:
  `selfupdate/archive`, `service/scm`, `verify/signednote`, `verify/ghattest`.

### Consequences

* Good, because Magic runs on each library's current release, with the toolchain advisory closed
  in the library's release tools too.
* Good, because a dead workaround goes (D2), and so do two SemVer parsers and a hand-written
  comparison (D5).
* Good, because an update started from a phone session would no longer take the daemon down (D3),
  once reproduced and fixed.
* Good, because `setup-service` drains the Windows daemon as `update` does, and launchd teardown has
  one implementation, the library's (D4).
* Good, because `v1.16.0`'s faster encoding comes for free on every ACP turn.
* Neutral, because `agentproc` waits on one SDK change, and Magic's process code stays until then.
* Bad, because D2 is not done until `make live-grok` spends the owner's quota.
* Bad, because D3 and D4 change service behaviour the release contract and the live runs exercise,
  so each needs its own live check on Linux and Windows, like S9d and S10b.
* Bad, because D12's one-pin rule with gobble keeps not holding until gobble moves.

### Confirmation

* `go.mod` names `go-selfupdate-lib v1.17.2` and `acp-go-sdk2 v1.19.0`, `ci.yml`'s two pins name
  `036289ec…`, and `TestWorkflowPinsMatchGoMod` passes.
* `submitPrompt` has no `ctx.Err()` check, and `TestPromptSendsNothingWithAnEndedContext` passes.
* **F1's reproduction** fails before D3 and passes after, on the Linux test host. On Windows, the
  outcome is measured and recorded.
* **`setup-service --remove` on the Windows test host** ends the daemon with exit code 0, a drain,
  measured as in S9d.
* `provider.SameVersion` and opencode's pin checks run on `golang.org/x/mod/semver`, with 0186 P2's
  tests. `make check-libs` runs, with its verdicts recorded.

## Pros and Cons of the Options

### A — Pins, the handoff, the backends, 0186's work; `agentproc` waits for a hook; no `go-llmprovider-sdk`

* Good, because every adopted capability replaces Magic code or closes a defect.
* Good, because nothing Magic relies on is lost: `wirecap` keeps its capture.
* Neutral, because it is several phases, each bisectable to one change.
* Bad, because D3 and D4 need live checks on two hosts.

### B — Pins only

* Good, because it is the smallest change: two `go.mod` lines, two workflow pins, one deleted check.
* Bad, because F1 stays: an agent-run update can stop the daemon on Linux and Windows.
* Bad, because the duplicated code F2 and F3 name stays.

### C — Adopt `agentproc` now

* Good, because about four pieces of process code in `spawnAgent` and its teardown go at once.
* Bad, because `wirecap`'s ACP capture (0137), the evidence behind the wire fixtures, has no seam in
  `agentproc` and would be lost.
* Bad, because forking or wrapping the SDK to keep it would be a duplication of its own.

### D — Adopt `go-llmprovider-sdk`

* Good, because Magic and gobble would share one LLM client.
* Bad, because no Magic code would go: Magic calls no vendor API (the table above).
* Bad, because it reverses 0185 for nothing measured, and the earlier risks still apply (0183).

## More Information

### Questions for the owner

1. **Where the work lives.** The proposal is one PLAN, 0207, with phases:
   * the self-update pin (D1);
   * the ACP pin, as 0184's B4 (D2);
   * the handoff (D3);
   * the backends (D4);
   * 0186's P1 and P2 (D5);
   * the minor items (D8).

   The alternative is to amend 0054's, 0184's and 0186's PLANs, each with its own part.
2. **Quota.** D2's `make live-grok`: run it with D2, or later?
3. ~~**The SDK issue (D6).** File it on `acp-go-sdk2` now?~~ Filed on 2026-10-21 as
   `maccavelli/acp-go-sdk2#10`.
4. **Order.** D1 and D2 first, then D3 and D4 in one release, so the live checks run once?

### Evidence index

* Clones at the tags in the releases table: `go-selfupdate-lib` `v1.17.1` and `v1.17.2`;
  `acp-go-sdk2` `v1.15.0` and `v1.19.0`; `go-llmprovider-sdk` `v1.21.0`.
* `acp-go-sdk2` release notes `v1.15.1` to `v1.19.0`: GitHub releases API.
* `go-selfupdate-lib` `CHANGELOG.md` at `v1.17.2`, entry "v1.17.2 - 2026-10-21".
* The scratch pin run: `r0207/pin_probe.py` and its log, as quoted above.
* gobble-cli's `go.mod`: GitHub contents API, default branch, read 2026-10-21.

### Related records

* [0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md):
  D1 (pins), O1, D3, and "What stays deliberately unadopted", which D3 here amends.
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md):
  D12, and B3, which D2 replaces.
* [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md),
  which D7 keeps.
* [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md):
  D7, D8 and its PLAN's P1 and P2, which D5 completes.
* [0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md](../reports/0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md).
* [0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md](0205-MADR-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md).
