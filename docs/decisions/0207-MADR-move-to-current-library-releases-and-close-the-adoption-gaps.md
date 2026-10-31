---
status: accepted
date: 2026-10-30
decision-makers: Project Owner
consulted: "go-selfupdate-lib v1.17.1–v1.17.2, acp-go-sdk2 v1.15.0–v1.19.0 and go-llmprovider-sdk v1.21.0, read from full clones at their tags; their GitHub release notes; a scratch copy of this repository at 707d300d with both pins moved; gobble-cli's go.mod on its default branch. Amendment of 2026-10-22: acp-go-sdk2 v1.20.0 from the Go module proxy and its release notes; a scratch copy at 6a4ef48d with both pins moved and agentproc run against Magic's command; gobble-cli at 53565a1. Amendment C (2026-10-24): go-selfupdate-lib v1.20.0 and v1.21.0, their CHANGELOG entries, migration guide §20 and §21, and `git diff v1.19.0 v1.21.0` in a local clone; the Go module proxy; a scratch copy of this repository at 793b8f5b with the pin moved. Amendment E (2026-10-24): go-selfupdate-lib v1.22.0 from the Go module proxy and a local clone; acp-go-sdk2 through the GitHub compare API; go-llmprovider-sdk v1.22.0 to v1.22.2 in a local clone; gobble-cli's go.mod; a scratch copy at 793b8f5b with the pin moved to v1.22.0"
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
  * *Note, 2026-10-27, D27, decided by the owner:* no longer true. gobble-cli's `go.mod`, at its
    default branch's `6ee20140` (2026-10-23), pins `acp-go-sdk2 v1.20.0`, as Magic does, so 0184
    D12 holds. gobble pins `go-selfupdate-lib v1.17.0`, which D12 does not cover.

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

*Accepted 2026-10-27.* The owner asked "what is pending in the madr?", and answered the questions
that could be decided then; for those that could not, the owner said to take the recommended first
option, which was to accept with them deferred:

* **Decided and carried out:** D1 to D23, D27 and D29 to D44, through these plans:
  * [0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md);
  * [0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md](0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md);
  * [0207-PLAN-finish-full-semver-pins-and-check-libs.md](0207-PLAN-finish-full-semver-pins-and-check-libs.md);
  * [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md),
    whose last steps are U6, U6b and U1c;
  * 0210, for D3 and D23;
  * [0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md](0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md)'s
    C4 alone, for D27.
* **Deferred: D24, D25 and D26.** D24 says `acp-go-sdk2 v1.20.0` is the newest tag. Since
  2026-10-24, `v1.21.0`, `v1.22.0`, `v1.23.0` and `v1.24.0` have shipped (`git ls-remote --tags`,
  2026-10-27), and D25 and D26 were read against `v1.20.0`. The owner chose "Assess v1.21–v1.24
  first (Recommended)":
  * a later amendment of this record reads those releases against Magic, as amendment E did for
    the other libraries, and re-proposes the pin, D25 and D26 on the newest release;
  * until then, the SDK-gaps plan's C1 to C3 do not run, and 0184's PLAN keeps its one open item.
  * *Note, 2026-10-28:* amendment H, below, is that assessment, read to `v1.25.0`. The owner
    decided it the same day: D24 is superseded by D45, and D25, D26, D45 to D47 are decided.
  * *Note, 2026-10-29:* the SDK-gaps plan ran C0 to C3 and is `complete`. C3's live run closed
    0184's open item, with the 0184 MADR's amendment of 2026-10-29.
* **D28:** question 12 answered "None yet (Recommended)". Each feature takes its own MADR when
  wanted, `ResumeSession` first.
* **Question 4** (order) was never put. It is moot: every phase it ordered has run.

### The decisions (proposed)

* **D1 — Pin `go-selfupdate-lib v1.17.2`.** `go.mod` and both `ci.yml` pins move together, the pins
  to `036289ecc701d84c7749f4950a57a154c1af2cf4` with the comment `# go-selfupdate-lib v1.17.2`
  (0054 D1; `TestWorkflowPinsMatchGoMod`). No Go code changes.
  * *Note, 2026-10-22.* Approved by the owner with its plan,
    [0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md)
    ("Recommended for any and proceed"). This record stays `proposed` for D3, D4 and D8's
    `binaryWarning` half.
    * `git ls-remote … 'refs/tags/v1.17.2^{}'` still gives `036289ec…`, and no newer tag exists.
    * The move changed `go.mod:12`, `go.sum:70-71` and `ci.yml:712` and `:733`, and nothing else.
    * The plan's execution record holds the evidence.
* **D2 — Pin `acp-go-sdk2` ~~`v1.19.0`~~ `v1.20.0`, replacing 0184's B3.** The tag moved to
  `v1.20.0` in the amendment of 2026-10-22; the steps below are unchanged. It is a 0184 B phase, as B1 and B2 were,
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
  * *Note, 2026-10-22.* D3 is assessed in full, and proposed, by
    [0210-MADR-hand-off-agent-run-updates-outside-the-service.md](0210-MADR-hand-off-agent-run-updates-outside-the-service.md).
    It corrects F1 (with the 0207 REPORT's A1) and this record's amendment note on D3. It also
    adds that runit, s6 and OpenRC have no inside guard at all.
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
  * *Note, 2026-10-22.* D5 is carried out by its own plan,
    [0207-PLAN-finish-full-semver-pins-and-check-libs.md](0207-PLAN-finish-full-semver-pins-and-check-libs.md).
    It was read against the tree, which differs from 0186's text in three ways:
    * kilo has no parser copy. It compared the version string exactly (`kilo/version.go:42`).
    * Three tests asserted the reverse of 0186 D8, that a prerelease matches its release.
    * `x/mod/semver` reads a bare `v1` as `v1.0.0`. Magic keeps its rule that a version needs
      `major.minor`.

    The owner's answers: kilo moves onto the shared comparison; a prerelease of opencode's floor is
    below the floor; and the work is two phases, SemVer first.
* ~~**D6 — `agentproc` is not adopted yet; the SDK is asked for an output hook (C, declined).**~~
  **Superseded by D10 in the amendment of 2026-10-22:** `v1.20.0` ships the hook as
  `Command.OutputTap`, issue #10 is closed, and `agentproc` is adopted. The original text follows.
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
    `conn.Err()`. Extended to the process's exit by D11 in the amendment of 2026-10-22.
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
* Bad, because D12's one-pin rule with gobble keeps not holding until gobble moves. *Note,
  2026-10-27, D27: no longer true; gobble moved to `v1.20.0` (G6).*

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

   *Answered 2026-10-22: "A second 0207 PLAN (Recommended)".* The SDK work (D2, D10 to D13) is
   [0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md](0207-PLAN-adopt-acp-go-sdk2-v1-20-0-and-agentproc.md).
   D1, D3, D4, D5 and D8's `binaryWarning` half take a sibling 0207 PLAN.
2. **Quota.** D2's `make live-grok`: run it with D2, or later?
   *Answered 2026-10-22: "Once, at S6 (Recommended)":* one run, after the SDK plan's S1 to S5.
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

## Amendment — 2026-10-22: `acp-go-sdk2 v1.20.0`, `agentproc`, the connection's end, and two connection hooks

**The owner's ask, verbatim:** "Amend 207, adopt 1-5, open an issue about item 3 in gobble-cli repo.
Pin to latest. Ground in codebase and research facts and ensure updated information is accurate,
detailed, and proves all assertions."

Items 1 to 5 are a list of unused SDK functionality given to the owner in conversation earlier the
same day:

1. `agentproc` with `OutputTap`;
2. the connection's end cause, `Err` and `Wait`;
3. agent-reported state in `session_info_update`;
4. `WithPromptOverlapPolicy`;
5. `WithNotificationDropHandler` and `WithInboundValidation`.

Checked against the source, items 3 and 4 were wrong as stated (G4, G5).

* **Item 3.** The owner was told and chose "No issue; fold into 0195 (Recommended)". No gobble-cli
  issue was filed.
* **Item 4.** Nothing on Magic's side can adopt it (D14).

### Releases [observed 2026-10-22, the GitHub releases and git refs APIs]

| Library | Magic pins | Latest | Latest's commit | Published |
| :--- | :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | `v1.15.0` | **`v1.20.0`** | `4fa720471d44bdcbe3f7318d494d95035708fe12` (tag object `686a420d56cbaf3a70bccf5530604201a5e0394b`) | 2026-10-22T03:18:04Z, immutable |
| `go-selfupdate-lib` | `v1.17.1` | `v1.17.2`, unchanged since D1 | `036289ecc701d84c7749f4950a57a154c1af2cf4` | tag only |

* **The source read is the proxy's module.** `go mod download -json
  github.com/maccavelli/acp-go-sdk2@v1.20.0` gives:
  * `"Sum": "h1:RSZL1Kvpm1kAJdSPmoTC012074XZjCRwh7P2V/Lt16U="`;
  * `"Origin"` hash `4fa72047…`, ref `refs/tags/v1.20.0`.

  Every SDK line number below is in that tree.
* **Issue `maccavelli/acp-go-sdk2#10` is closed,** at 2026-10-22T03:18:11Z, seven seconds after the
  release.
* **The ACP schema is still `1.25.0`.** The release notes say "`git diff v1.19.0 -- schema/` is
  empty", and "`make api` reports four compatible additions and no incompatible change".

### What `v1.20.0` changes, against Magic's use

| Change (the release notes) | Effect on Magic |
| :--- | :--- |
| `agentproc`: `Command.OutputTap` and `InputTap`, `Process.DroppedTapBytes` | The seam D6 asked for. D10 adopts it. |
| `agentproc`: `Process.Kill`, at once, with `ErrKilled` | Today's teardown semantics, through the library (G1, D10). |
| `agentproc`: the agent's Windows job allows breakaway | Removes Magic's own agent job as the obstacle F1 names for D3 on Windows (G1). |
| `agentproc`: on Windows, the agent starts suspended until it is in its job | Closes a race Magic's `SuperviseStarted` order has (G1). |
| `agentproc`: on Linux and FreeBSD, `Pdeathsig` SIGKILL, from a thread locked for the agent's life | ACP agents get client-death cleanup they lack today (G1). |
| The client records `initialize` when its answer arrives, and holds what the agent sends before it | A message an agent writes with its answer is no longer refused. Held notifications meet the queue bound: Magic runs `OverflowDropNewest` (`acpagent.go:1049`), so a burst drops, never closes. |
| The validation hook sees the `initialize` answer on the reader goroutine | D13's hook must not block (G3). |
| A 128 KiB read buffer, 64 KiB more per connection | One connection per agent process; nothing to change. |

### Findings

* **G1 — `agentproc` now fits `spawnAgent`.**
  * **The command's contract** (`agentproc/agentproc.go:33-39`):
    * `Stdin` and `Stdout` must be nil, so the two pipes `spawnAgent` makes (`acpagent.go:470-477`)
      go.
    * `Path`, `Args`, `Env`, `Dir` and `Stderr` stay the caller's. So the command line
      `launch.Command` builds (`acpagent.go:464`), `cmd.Dir = procDir` (`:468`) and the stderr
      scraper (`:498-503`) keep working.
  * **Unix.** `prepare` (`agentproc/tree_unix.go:21-33`):
    * refuses only a preset `Pgid`, and sets `Setpgid` unless `Setsid` is set;
    * Magic's command already has `Setpgid` (`procutil/procutil_unix.go:25-30`);
    * the probe below starts Magic's command with it, in a process group of its own.
  * **Linux.** `setDeathSignal` sets `Pdeathsig` to SIGKILL unless the caller set one
    (`agentproc/deathsig_pdeathsig.go:12-16`).
    * `acpagent` sets none today. The only callers of `procutil.SetDeathSignal` are
      `codex/provider.go:566`, `httpagent/provider.go:518` and `providerauth/cli.go:100`.
    * `agentproc` starts the agent from a goroutine locked to its OS thread until the agent is
      reaped (`agentproc.go:206-224`). That is the hazard `procutil`'s own comment names, "the Go
      runtime may retire that thread while the process lives on, which silently disarms it"
      (`procutil/owner_linux.go:18-21`).
  * **Windows.**
    * **`prepare`** (`agentproc/tree_windows.go:31-40`) adds `CREATE_SUSPENDED`, and refuses a
      command that already has it. It keeps the caller's other flags, so `procutil`'s
      `CREATE_NEW_PROCESS_GROUP` and conditional `CREATE_NO_WINDOW` stay
      (`procutil/procutil_windows.go:103-118`). It does not touch `SysProcAttr.CmdLine`, which
      `launch.Command` sets for a batch shim (0159 D23).
    * **`attach`** (`:66-94`) creates the job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
      JOB_OBJECT_LIMIT_BREAKAWAY_OK`, assigns the agent, then resumes it.
    * **Magic's job differs.** `procutil.SuperviseStarted` (`procutil/procutil_windows.go:261-279`)
      creates the job with `KILL_ON_JOB_CLOSE` alone, after `cmd.Start` has returned
      (`acpagent.go:504`, `:512`). The release notes measured that order letting a descendant
      escape the job, "with the assignment 100 ms late, in 20 of 20 trials". Magic's gap is
      shorter, and this was not reproduced on Magic.
  * **`wirecap` needs a writer.** `OutputTap` is an `io.Writer` that gets the raw bytes on a
    goroutine of its own and "must not retain p" (`agentproc.go:48-58`). `wirecap` today wraps the
    reader instead (`wirecap/wirecap.go:161-193`).
    * Its line splitting moves to a `Write` method, which copies (`append`), so nothing is
      retained.
    * A disabled capture must be passed as an untyped nil: a typed nil `*Capture` in the interface
      is a set tap.
  * **The process and the connection are tied by the library.**
    * When the agent exits, `watch` (`agentproc.go:259-273`) kills what is left of its group or job
      and closes the connection.
    * When the connection ends first, `monitor` (`:277-283`) stops the agent.
    * Magic does the first by hand, with the `cmd.Wait` watcher (`acpagent.go:600-618`) and
      `procExited` (`session.go:148-151`). It never stops the agent on a connection end; the
      manager's `Close` does that later. Magic still needs its `disconnected` event, so
      `watchConnClose` and `signalDisconnected` (`session.go:1213-1252`) stay.
  * **Teardown.**
    * **Magic today** sends SIGKILL to the group straight after `conn.Close`
      (`session.go:1359-1379`).
    * **`Process.Close`** is gentler. It closes the input and waits up to the grace; on Unix it
      sends SIGTERM and waits again; then it kills (`agentproc.go:356-395`). Windows has no SIGTERM
      step (`tree_windows.go:136`).
    * **`Process.Kill`** goes straight to the kill (`:367-380`).
* **G2 — The connection's end cause is available on Magic's connection type.**
  `ClientSideConnection.Err` and `Wait` (`client_gen.go:72`, `:78`) forward to `Connection.Err`
  (`connection.go:772`) and `Wait` (`:788`). `Err` returns one of two errors:
  * `ErrConnectionClosed` when this side ended the connection, wrapping
    `ErrNotificationQueueOverflow` for an overflow;
  * otherwise `ErrPeerDisconnected`, "wrapping how the peer ended it, io.EOF at the end of its output
    or the read's error".

  Today the message is the fixed "agent connection lost" (`session.go:1251`). The process exit's
  message is "`<id>` process exited: `<err>`" (`acpagent.go:613-616`). It cannot tell Magic's own
  kill from the agent's crash.
* **G3 — The two hooks.**
  * **The drop handler.**
    * `WithNotificationDropHandler` (`connection.go:542-555`) runs on the reader goroutine. It "must
      return quickly and must not block".
    * 0167 D17 declined it for that reason, and D18 tells the user per turn from
      `DroppedNotifications()` (`session.go:616-657`). D18 knows how many updates were dropped, not
      which method.
  * **Inbound validation.**
    * **Without the hook,** a `session/update` the SDK refuses is dropped and logged "failed to handle
      notification" at Warn (`connection.go:1683-1697`). Magic's log gets it through `WithLogger`;
      the phone is told nothing.
    * **Where the hook runs.** For `session/update`, `checkInbound` (`connection.go:2319-2344`) runs
      the hook in the dispatch, after decode and before Magic's `SessionUpdate`
      (`client_gen.go:258-264`). Its `params` is the `*SessionNotification`, which carries the
      session id. For the `initialize` answer it runs on the reader (the release notes).
    * **What the hook cannot see.** A message that does not decode reaches no hook
      (`connection.go:574-575`).
* **G4 — Item 3, as stated, was wrong.**
  * **No root-session state.** `session_info_update` has no `state`: `SessionSessionInfoUpdate`
    has `_meta`, `sessionUpdate`, `title` and `updatedAt` only (`types_gen.go:14537-14549`).
  * **Only on `subagent_update`, and unstable.**
    * `StateUpdate` is a field of exactly two types: `SessionSubagentUpdate` (`:14866`) and
      `SubagentUpdate` (`:17038`).
    * Both are marked "**UNSTABLE** … not part of the spec yet" (`:14830-14832`).
    * `schema/schema.json` has no `StateUpdate` (0 matches). `schema/schema.unstable.json` defines
      it (`:5995`) and uses it in `SubagentUpdate.state` (`:6364`, `:6398`).
  * **No engine Magic runs sends it.**
    * gobble sends `session_info_update` with a title only (`acpserver/commands.go:597`).
    * gobble has no subagent code: 0 files match `subagent` at `53565a1`, and GitHub code search of
      the repository returns `total_count` 0.
    * 0195 already maps subagent `state` (its lines 26-48) and defers it until an engine sends it.
  * **Magic already has the root session's status from the protocol itself.**
    * `running` when it sends the prompt (`session.go:408-413`).
    * `idle` when the prompt's answer returns (`:527`, `:547`).
    * Waiting on the user when the agent sends `session/request_permission` (`:2337`).
* **G5 — Item 4 does not apply to a client.**
  * `WithPromptOverlapPolicy` "sets what an AgentSideConnection does … Other connections ignore
    it" (`connection.go:465-468`). It is read only at `agent_gen.go:172`.
  * gobble, an agent, already sets `PromptOverlapConcurrent` (`acpserver/serve.go:38`).
  * Magic's own per-session queue (`session.go:327`, `tryDrainQueue` at `:661`) is the client's
    side, and stays.
* **G6 — The one-pin rule with gobble still does not hold.** gobble pins `acp-go-sdk2 v1.16.3`
  (`go.mod:15` at `53565a1`). Moving it is gobble's (0184 F4, G5).
  * *Note, 2026-10-27, D27:* no longer true; gobble pins `v1.20.0` at `6ee20140` (the note under
    "acp-go-sdk2, `v1.15.0` → `v1.19.0`" above).

### The pin move and `agentproc`, proven on a scratch copy [2026-10-22]

**Setup.**

* The copy holds the tracked files as they stood in the working tree: `6a4ef48d`, with 0208's P3
  staged and `apps/` left out.
* Its build and module caches are its own.
* Nothing in the tree was written.
* The script is `r0207b/pin_probe.py`, with go1.27.3 on darwin/arm64.

**What the copy changed.**

* Both pins and `ci.yml`'s two workflow pins moved to `036289ec…` (D1).
* `submitPrompt`'s mitigation was then removed, for B3's premise.
* `zz_agentproc_probe_test.go` was added to `acpagent`. It starts the SDK's example agent, built
  from the same module, through Magic's own `launch.Resolve` and `launch.Command`, under
  `agentproc` with `clientConnOptions` and an `OutputTap`.

```text
go: upgraded github.com/maccavelli/go-selfupdate-lib v1.17.1 => v1.17.2
go: upgraded github.com/maccavelli/acp-go-sdk2 v1.15.0 => v1.20.0
go mod tidy: exit 0
.github/workflows/ci.yml | 4 ++--     go.mod | 4 ++--     go.sum | 8 ++++----
go build ./...: exit 0
go vet darwin/arm64, windows/amd64, linux/amd64: exit 0
go test -race ./...: exit 0; 43 ok, 0 FAIL lines
B3 premise on v1.20.0, mitigation removed:  --- PASS: TestPromptSendsNothingWithAnEndedContext
B3 premise on v1.15.0, mitigation removed:  lifecycle_test.go:77: a prompt with an ended context put "session/cancel" on the wire
=== RUN   TestZZProbeStartTapClose
    SysProcAttr before Start: Setpgid=true Pgid=0
    agent pid=60315 pgid=60315 (own group: true)
    initialize answered, protocolVersion=1
    Close returned after 1ms: <nil>
    DroppedTapBytes: output=0 input=0
    tap's first line: {"jsonrpc":"2.0","id":1,"result":{"agentCapabilities":{...
--- PASS
=== RUN   TestZZProbeKill
    Kill returned after 1ms: agentproc: agent killed: signal: killed (ErrKilled: true)
--- PASS
=== RUN   TestZZProbeAgentDiesConnErr
    Wait after the agent died: signal: killed
    Conn().Err() after the agent died: acp: peer disconnected: peer connection closed
    errors.Is ErrPeerDisconnected=true, io.EOF=true
--- PASS
=== RUN   TestZZProbeRefusesPresetPgid
    Start with a preset Pgid: agentproc: Command.Cmd.SysProcAttr.Pgid is set; the agent needs a process group of its own
--- PASS
```

**What it shows.**

* **The pin moves cleanly.** `go.mod` changes two lines, the two requirements. With `ci.yml`'s pins
  moved as D1 requires, the whole race suite passes; the one failure of 2026-10-21,
  `TestWorkflowPinsMatchGoMod`, came only from leaving them.
* **B3's premise holds on `v1.20.0`.**
* **Magic's command starts under `agentproc`, unchanged.** The agent gets a process group of its
  own, and the tap holds the agent's raw `initialize` answer with no bytes dropped.
* **The negative control is real.** A preset `Pgid` is refused, so the start above is not vacuous.
* **D11's distinction exists.**
  * Magic's own `Kill` reports `ErrKilled`.
  * An agent killed from outside reports the bare exit status. The connection then says
    `ErrPeerDisconnected`, wrapping `io.EOF`.
* **Not shown here:**
  * Linux's `Pdeathsig` and Windows' job, suspended start and breakaway. Those rest on the SDK's
    source and its own tests (the release notes), not on a Magic run.
  * The example agent exits at its input's end, so `Close` took 1 ms. Real engines may not, which
    is question 6.

### The decisions this amendment adds (proposed)

* **D2, as amended — pin `v1.20.0`.**
  * Its steps are unchanged: remove `submitPrompt`'s mitigation, keeping its test.
  * It is done only when `make live-grok` passes, which spends quota and waits for the owner.
* **D10 — Adopt `agentproc` in `spawnAgent` (item 1; replaces D6).**
  * **The start.** The agent starts through `agentproc.Command`. `Cmd` is the command
    `launch.Command` builds, with `Dir` and `Stderr` as today. The rest:
    * `Grace` is set;
    * `ConnectionOptions` is `clientConnOptions(log)`;
    * `OutputTap` is the wire capture's writer when `wirecap` is enabled, and nil otherwise;
    * `Start(s)` is called, and `s.conn` becomes `Conn()`.
  * **Leaves `acpagent`:** the two pipes; `procutil.SuperviseStarted` and `s.release`; the group
    kills in `markClosedAndKill`, `Close` and `killProcessTree`; `procExited`; and the `cmd.Wait`
    watcher. The watcher is replaced by `Done` and `Wait`, and spawn's failure paths call `Kill`.
  * **Stays in `procutil`:** its functions keep their other callers (codex, httpagent,
    providerauth).
  * **`wirecap`** gains the writer of G1. `TeeReader` goes once nothing calls it.
  * **Teardown keeps today's semantics:** `Close` calls `Kill` after `session/close`. A graceful
    `Process.Close` is question 6.
  * **`InputTap` is not wired:** nothing in the repository reads client-to-agent frames.
* **D11 — The phone is told why the agent went away (item 2; extends D8).**
  * `watchConnClose`'s message carries `conn.Err()`.
  * The exit watcher's message carries `Wait`'s error. That error names `ErrKilled` or
    `ErrTerminated` when Magic stopped the agent, and the exit status when the agent stopped by
    itself.
  * *Note, 2026-10-22 (the SDK plan's S4 deviation 1):* on an agent's death the connection's end
    was reported first in 28 of 30 scratch runs, since `agentproc`'s `Wait` waits for the
    connection's goroutines. So `watchConnClose` waits up to 500 ms for the process to exit, and
    defers to the exit watcher when it does.
* **D12 — A drop handler that only records (item 5, first half; amends 0167 D17).**
  * The handler stores the dropped notification's method in an atomic value on the connection's
    owner. It does no I/O, takes no lock, and calls nothing that can wait, which keeps D17's reason.
  * D18's per-turn notice names the method.
  * 0167 D17's "with no drop handler" becomes "with a handler that only records".
* **D13 — Refused agent messages reach the phone (item 5, second half).**
  * A `WithInboundValidation` hook returns the SDK's verdict unchanged, so nothing the SDK refuses
    is accepted.
  * It counts each refusal: against the session where `params` carries a session id, otherwise
    against the connection.
  * The turn's end emits one notice, as D18 does for drops, naming the count and the method. The
    text says the agent sent updates that broke the protocol and were dropped.
  * The hook must not block, since it runs on the reader for the `initialize` answer.
* **D14 — Items 3 and 4 are not adopted, for the reasons in G4 and G5.** Subagent state stays with
  [0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md](0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md).
  No gobble-cli issue is filed, at the owner's choice.
* **D3, noted.** `agentproc`'s job allows breakaway (`tree_windows.go:73`), so D3's Windows
  measurement no longer has Magic's agent job in the way. The measurement still runs: whatever
  starts the daemon may hold a job of its own.

### Consequences of the amendment

* Good, because about six pieces of hand-written process code in `acpagent` go, replaced by a
  library that tests them on three platforms.
* Good, because ACP agents gain client-death cleanup on Linux, and a job assignment with no window
  on Windows.
* Good, because the wire capture survives the move, and the phone learns why an agent went away and
  when an agent sent something the protocol forbids.
* Neutral, because `acpagent`'s teardown keeps its semantics: `Kill`, as today's SIGKILL.
* Bad, because the change sits on the one path every ACP session takes. It needs the full
  `acpagent` suite, `make ci-windows` on the Windows host, and `make live-grok`, which spends quota.
* Bad, because D12 reverses part of a recorded decision (0167 D17), for a modest gain: the method
  name in a notice.

### Confirmation of the amendment

* `go.mod` names `acp-go-sdk2 v1.20.0`.
* `acpagent` calls none of `procutil.SuperviseStarted`, `procutil.KillProcessGroup`,
  `cmd.StdinPipe` or `cmd.StdoutPipe`.
* A test starts a helper agent through `spawnAgent` and shows the agent in its own process group on
  Unix, the capture holding the raw `initialize` answer, and `Close` ending the group.
* Fail-first tests, each seen failing on a scratch copy before it passes:
  * A dropped notification's notice names its method.
  * A `session/update` the SDK refuses produces the notice of D13.
  * The `disconnected` error carries the cause: `ErrPeerDisconnected` wrapping `io.EOF` when the
    agent dies.
* On the Windows test host, `make ci-windows` passes, and an agent's descendant ends with the job.
* `make live-grok` passes, run when the owner approves the quota.

### Questions this amendment adds

These continue the numbering of "Questions for the owner" above.

* **5. Where the work lives.** D10 to D13 join D2's phase in the 0207 PLAN, as one `acp-go-sdk2`
  phase, or follow it as a phase of their own, so the pin and the process change bisect apart?
  The proposal is a phase of their own.
  *Answered 2026-10-22: "Separate phases (Recommended)".*
* **6. Graceful teardown.** Keep `Kill`, today's semantics, or use `Process.Close` with a short
  grace, so an agent that saves its session at its input's end can do so? `Close` waits up to two
  graces on Unix. The proposal is `Kill` now, and `Close` once an engine is shown to lose state to
  SIGKILL.
  *Answered 2026-10-22: "Kill at once (Recommended)".*

### Evidence index of the amendment

* `acp-go-sdk2 v1.20.0`: the module from the Go module proxy (sum above), and the release notes,
  from the GitHub releases API.
* Issue `maccavelli/acp-go-sdk2#10`: `gh issue view`, closed at 2026-10-22T03:18:11Z.
* gobble-cli at `53565a1`, read locally, and its GitHub code search.
* The scratch probe: `r0207b/pin_probe.py`, `r0207b/zz_agentproc_probe_test.go` and the log quoted
  above.

### Related records of the amendment

* [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md):
  D17, which D12 amends, and D18, which D12 and D13 extend.
* [0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md](0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md):
  where subagent state stays (D14).
* [0150-MADR-the-windows-tree-kill-was-never-wired-up.md](0150-MADR-the-windows-tree-kill-was-never-wired-up.md):
  the job object `agentproc` now provides for ACP agents.
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md):
  the wire capture D10 keeps.
* [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md):
  pass-through first, which D11 and D13 serve.

## Amendment — 2026-10-23 (B): `go-selfupdate-lib v1.19.0`, and the fullest use of both libraries

**Status of this amendment: proposed.** The owner answered questions 8 to 11 on 2026-10-23 (below).
*Accepted 2026-10-27 with the record, D24 to D26 deferred (the note under the Decision
Outcome).*
It becomes decided when the owner approves its PLANs.

*Note, 2026-10-23.* The owner approved the `go-selfupdate-lib` PLAN's U0 to U2 ("committed,
proceed with U0–U2"). So D15 (the pin), D18's issue
([go-selfupdate-lib#8](https://github.com/maccavelli/go-selfupdate-lib/issues/8)) and D21 (the
Windows CI script) are carried out; that PLAN's execution record holds the evidence. The rest
stays proposed.

**The owner's ask, verbatim:** "assess the go-llmprovider-sdk, acp-go-sdk2, and ensure we pin the
current versions, and are using the fullest feature sets we can." Corrected in the next message:
"go-selfupdate-lib is the one". So this amendment covers `go-selfupdate-lib` and `acp-go-sdk2`.
`go-llmprovider-sdk` stays out of scope (D7; 0185), and was not re-assessed.

### Releases [observed 2026-10-23, the GitHub tags, releases and compare APIs, and the Go module proxy]

| Library | Magic pins | Latest | Latest's commit | Published |
| :--- | :--- | :--- | :--- | :--- |
| `go-selfupdate-lib` | `v1.17.2` | **`v1.19.0`** | `a6af9dc760a5392134dc62b00e6491b112d0ada5` | tag only, 2026-10-23; `v1.18.0` (`ce406dc1…`) the same day |
| `acp-go-sdk2` | `v1.20.0` | `v1.20.0`, unchanged | `4fa720471d44bdcbe3f7318d494d95035708fe12` | 2026-10-22, immutable |

* **`go-selfupdate-lib`.** `go mod download -json …@v1.19.0` gives `"Sum":
  "h1:u08KixmhNgPv72HGOzNUvqxEU2CCY8xEU2Fb7vN7j58="` and origin `a6af9dc7…`, ref
  `refs/tags/v1.19.0`. The library still publishes no GitHub releases (`gh release list` is
  empty). Its `go.mod` asks for `go 1.26.10`, `golang.org/x/mod v0.40.0`, `x/sys v0.47.0` and
  `x/term v0.43.0`, all at or below Magic's.
* **The reusable release workflows did not change.** `compare/v1.17.2...v1.19.0` lists no file
  under `.github/workflows/`. So `ci.yml`'s two pins move by commit and comment only.
* **`acp-go-sdk2`.** 26 commits follow the `v1.20.0` tag. The only Go file they change is
  `agentproc/tap_test.go`, a test. `go.mod`, `schema/` and `version` are unchanged. Nothing
  unreleased matters to Magic.

### What `v1.18.0` and `v1.19.0` add, against Magic's code

Each release is additions only, by the library's `make apicheck` (its CHANGELOG). Each answers an
issue this repository filed from the REPORT's A10.

| Release | Addition (CHANGELOG; migration guide §18, §19) | Magic's counterpart today |
| :--- | :--- | :--- |
| `v1.18.0`, #5 | `selfupdate.DefaultHTTPClient()`: transport timeouts (30 s dial, 10 s TLS, 30 s response headers) and no `Client.Timeout` | `&http.Client{Timeout: OperationTimeout}`, 15 minutes over each request (`internal/updateclient/client.go:93`), the shape the library's guide now advises against (REPORT A5) |
| `v1.18.0`, #4 | `launchd.Job.Loaded`; `launchd.Job.Status`, with `Installed`, `Loaded`, `Enabled`, `Running` and `PID` from one probe | `ProbeStatus` runs `launchctl print` itself for "loaded" (`internal/cli/service/control.go:153-157`); `launchdLoaded` and `launchdJobRunning` parse `launchctl print` (`launchd_wait.go:54-57`, `:103-112`) |
| `v1.19.0`, #3 | `taskscheduler.Task.Register`, `Definition` and `Delete`, through the COM API | `schtasks /create /xml` from a UTF-16 temporary file (`setup_schtasks.go:48-63`; `refresh_schtasks.go:145-162`; `encodeTaskXML`, `schtasks.go:195`), `/query /xml` (`setup_schtasks.go:26`; `refresh_schtasks.go:38`) and `/delete` (`setup_schtasks.go:91`) |
| `v1.19.0`, #3 | `taskscheduler.DiffDefinitions`, `SameDefinition` and `DefinitionDiff` | `sameTaskDefinition`, `taskFields` and `taskParse` (`setup_schtasks.go:134-318`). The library's comparison covers every trigger, setting and action element, not a chosen set. Its fixtures include a capture of Magic's own task shape (`testdata/definitions/consumer.*.xml`) |
| `v1.19.0`, #3 | `taskscheduler.Task.Status`: installed, enabled, `TASK_STATE`, running, PIDs and the last run, in one call | `taskState`, a PowerShell `Get-ScheduledTask` of about 1.3 s per call (`control_schtasks.go:25-44`; 0159 probe 11), called from `isInstalledWindows`, `stopWindows`, `refreshSchtasks` and `uninstall.go:219` |
| `v1.19.0`, #6 | `Task.Detach` hands off as a one-shot task, `<Name>.selfupdate.<id>`, started by Task Scheduler outside the program's jobs; `Task.CleanupHandOffs`; `Options.HopDetach` and `HandOffDir` | The Windows handoff 0210 D3 waits on: `detacherFor` returns false on Windows (`client.go:205-211`), and the daemon cleans finished handoffs on macOS only (`internal/daemon/daemon_handoff.go`) |
| `v1.19.0` | `service.LoadHandOffEnvContext` applies and removes a first argument `--selfupdate-handoff-env=<path>` | Both `main`s already call it first (`cmd/mcremote/main.go:24`, `cmd/mcrelay/main.go:25`). Nothing to change. |

**What the library still lacks for Magic.** `taskscheduler.Task` has no exported way to enable or
disable a task outside `Stop` and `Start` (its exported methods, read at `v1.19.0`). Its `Start`
re-enables only a task its own `Stop` disabled (`lifecycle.go:272-299`). Magic's `startWindows`
enables first, whoever disabled it (`control_schtasks.go:60-68`). runit, s6 and OpenRC backends
remain [go-selfupdate-lib#2](https://github.com/maccavelli/go-selfupdate-lib/issues/2), open.

### The pin move, proven on a scratch copy [2026-10-23]

On a `git archive` copy of `HEAD` `88d37d1c`, given its own git index, nothing in the tree
written (`m207b/pin_probe.py` in the session scratchpad):

```text
go: upgraded github.com/maccavelli/go-selfupdate-lib v1.17.2 => v1.19.0
go mod tidy: exit 0
ci.yml: both pins moved
go build ./...: exit 0
go vet ./... darwin/arm64, linux/amd64, windows/amd64: exit 0, each
go test -count=1 ./...: exit 0; 43 packages ok, 0 FAIL lines
TestWorkflowPinsMatchGoMod, TestReleaseJobsReadTheSpec: ok
```

* `go.mod` changes on one line, and `go.sum` on the module's two lines.
* The first run failed one test, `TestTrackedFilesUseOnlyPlaceholderHomes`: "no git index … the
  guard cannot read the tracked files". The copy had no `.git`. With an index it passed. That is
  the probe's own artefact, not the pin's.

### What the 0207 REPORT left open, re-read against the tree at `88d37d1c`

[0207-REPORT-go-selfupdate-lib-deeper-adoption-assessment.md](../reports/0207-REPORT-go-selfupdate-lib-deeper-adoption-assessment.md):

| Finding | State |
| :--- | :--- |
| A1, the handoff | Done on systemd and launchd by 0210; Windows now unblocked (above); script supervisors wait on #2 |
| A2, `setup-service` on the backends | Open: 0207 D4 |
| A3, three ways to confirm a start | Open: no `WaitHealthy` or `PollHealthy` outside a test (`git grep`) |
| A4, the script backend's contract | Part done: 0210 H2 added `Inside`. Its context and sentinel errors are open |
| A5, the update's guards | Open: no `NewVersionProber`, `NewImageVerifier` or `DefaultHTTPClient` in the tree. `Spec.Unpacker()` returns nil for `binary` packaging (`releasespec/spec.go:329-330`), so `New` accepts the image verifier (`updater.go:105-121`) |
| A6, `binaryWarning` | Open: 0207 D8's first half; no `SameExecutable` in the tree |
| A7, the Windows CI script's stamps | Open: `scripts/ci-windows-local.ps1:132` still stamps `main.version`, so `make ci-windows-smoke`, a before-tag gate, cannot pass check B14 |
| A8, mcrelay readiness | Open, and never put to the owner (REPORT question 4) |
| A10, the four library asks | #3, #4 and #5 shipped (above); #2 open |

### `acp-go-sdk2 v1.20.0`, used against what it offers [read 2026-10-23]

The SDK's two exported packages, `acp` and `agentproc`, were read from the module cache, and every
capability was matched against `internal/provider/acpagent` and the records. Magic uses the
connection, every stable client call it has a reason to make, every client handler, the
connection hooks of D11 to D13, and `agentproc` with `OutputTap`. What it does not use, with no
recorded reason:

* **H1 — Per-turn token usage is dropped.** `PromptResponse.Usage`, marked unstable, carries
  input, output, cached-read, cached-write and thought tokens (`types_gen.go:11105-11120`). Magic
  reads only `StopReason` from the answer (`session.go:537-543`). gobble sends it (gobble-cli
  `acpserver/prompt.go`, read by the subagent; not run). So a non-grok agent's turns record no
  token split. `xaiusage.go:14-18` says ACP's standard usage carries no cache fields, which
  `v1.20.0`'s schema contradicts. Whether current grok fills the standard field as well as its
  `_meta` usage is **unverified**; grok 1.0.13's captures carry it only under `_meta`.
* **H2 — A missing file comes back as an internal error.** `ReadTextFile` returns the `PathError`
  (`session.go:2653-2665`), which the SDK sends as -32603. The SDK has `NewResourceNotFound`
  (-32002).
* **H3 — Hand-built protocol values the SDK constructs.** MCP server entries
  (`acpagent.go:639-649`, against `NewMcpServerHttp`, `NewMcpServerSse`), permission outcomes
  (`session.go:2471-2480`, `:2611-2620`, against `NewRequestPermissionOutcomeCancelled`,
  `…Selected`) and config-option requests (`session.go:1108-1112`, `:1168-1177`, against
  `NewSetSessionConfigOptionRequestValueId`, `…Boolean`). Same wire bytes; the type tags are then
  the SDK's.
* **H4 — Small diagnostics.** `Process.DroppedTapBytes` is never read, so a truncated wire capture
  is not known. A `ProtocolVersionError` at `initialize` (`acpagent.go:541-545`) is reported as
  any other error.
* **H5 — Dead branches.** The subagent decline branches (`session.go:2070-2075`) cannot run:
  since `v1.15.2` the SDK drops those updates when `subagents` is not advertised (0184 amendment
  of 2026-10-19, G3).

These are product features the SDK enables, each needing a decision of its own rather than an
adoption step: the typed `ResumeSession`, to re-attach without replay (grok and gobble advertise
`resume`; only 0167-REPORT L3 recommends it); resource-link and embedded-resource prompt blocks,
which both engines advertise (0179 F7, a finding only); confining file access to the session's
`Roots()` (0184 "More Information", an open question); URL elicitation (0191 D1, D6: a trust
policy first); stdio MCP servers (0179, deferred); grouped select options (0208 N3, proposed).

**Stale recorded facts** found on the way:

* 0207 G6 and this record's Consequences: "D12's one-pin rule with gobble keeps not holding".
  gobble-cli's `go.mod` now pins `acp-go-sdk2 v1.20.0` (fetched 2026-10-23), so 0184 D12 holds.
  gobble also pins `go-selfupdate-lib v1.17.0`, which D12 does not cover.
* [0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md](0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md)
  says the SDK does not gate the subagent variants. It has since `v1.15.2`. The deferral stands.
* [0167-REPORT-acp-go-sdk2-integration-assessment.md](../reports/0167-REPORT-acp-go-sdk2-integration-assessment.md)
  L3 calls `providers/*` editor-centric; those methods configure an agent's LLM providers. Its
  outcome stands, since no engine Magic runs advertises them. L4's "the SDK does not validate what
  it sends" was corrected by 0184 PLAN A5, and the report never was.
* [0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
  stays `in-progress` for one item: committed wire captures of permission, cancel, terminal,
  file-system, delete and resume frames. Only `grok/testdata/wire/1.0.13` and `1.0.50` exist.
  Closing it needs a capture-enabled `make live-grok`, which spends quota.
* **A risk to watch, not a finding:** the SDK's own
  `docs/reports/0032-REPORT-windows-floor-runtime-crashes.md` records Go runtime crashes in its
  root-package tests on Windows CI, 3 of 56 jobs, one at go1.27.3. Its cause is undecided.

### The decisions this amendment proposes

* **D15 — Pin `go-selfupdate-lib v1.19.0`.** `go.mod`, `go.sum`, and both `ci.yml` pins to
  `a6af9dc760a5392134dc62b00e6491b112d0ada5` with the comment `# go-selfupdate-lib v1.19.0`. No Go
  code changes. Proven above.
* **D16 — The update takes the library's guards (REPORT A5, A6).**
  * `New` passes `selfupdate.DefaultHTTPClient()` when no client is given. `cli.Run` already bounds
    the run by `OperationTimeout`.
  * `Config.Probes` gets `NewVersionProber` over the spec's `identity_args`, so a staged binary
    that cannot run here is refused before the service is stopped. On macOS with
    `MC_CODESIGN_IDENTITY` set, `codesign.NewChecker` as well.
  * `Config.Verifiers` gets `NewImageVerifier` for this platform.
  * `binaryWarning` uses `service.SameExecutable` (0207 D8's first half).
* **D17 — Task Scheduler through the library's COM API (`v1.19.0`, #3).**
  * `taskState` becomes `Task.Status`.
  * Register, read and delete become `Task.Register`, `Definition` and `Delete`. The UTF-16 file,
    `encodeTaskXML` and the `schtasks /query /xml`, `/create` and `/delete` calls go.
  * The comparison becomes `SameDefinition`. "Exists with different content" names the paths
    `DiffDefinitions` reports.
  * `taskFieldsFromXML` stays only where Magic reads a field (whether the task is disabled, the
    uninstall's ownership check, the refresh's recovery of options). The PLAN measures each call
    site.
* **D18 — Enabling a task: ask the library (question 9).** File a `go-selfupdate-lib` issue for an
  exported enable and disable on `taskscheduler.Task`. Until a release has it,
  `schtasks /change /enable` and `/disable` stay, Magic's last `schtasks.exe` calls.
* **D19 — launchd status through the library (`v1.18.0`, #4).** `ProbeStatus` reads
  `launchd.Job.Status` in one probe. `launchdLoaded` becomes `Job.Loaded`, which tells "not loaded"
  (exit 113) from a failure, the ambiguity 0125's open question 2 records.
* **D4, as amended — the backends' stop, start and start confirmation (REPORT A2, A3).**
  * Stop goes through the backend's `Stop`: on macOS the library's bootout-and-wait, on Windows its
    drain.
  * Start: on Windows, Magic enables the task (D18), then calls `Task.Start`. On macOS,
    `Job.Start` where it matches setup's path. The PLAN measures each call site first.
  * A start is confirmed by the backend's `WaitHealthy`, with the product's probe, for systemd,
    launchd and Task Scheduler, and by `service.PollHealthy` over the existing samplers for runit,
    s6 and OpenRC. *Amended 2026-10-27 by 0212-MADR amendment D (D22), decided: for runit, s6 and OpenRC,
    by the adapter's `WaitHealthy` once 0212's P5 builds it, with U6 after that phase.*
    *Note, 2026-10-27: 0212's P5 landed (`5c499ad0`), and its P6 live runs passed on all three
    (`dcd7b31f`). Nothing U6 waits on remains.*
  * `stopAndWaitDarwin`, `launchdJobRunning`, `startWindows`, `stopWindows` and the settle loops go
    when nothing calls them.
* **D20 — The script backend follows the backend contract (REPORT A4).** `Stop`, `Start` and
  `WaitHealthy` honour their context, and their errors wrap `service.ErrTimeout`, `ErrUnhealthy`
  and `ErrNotInstalled`.
  * *Note, 2026-10-27: retired into
    [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)
    (its R2), and met there rather than by changing the script backend.* 0212's P5 (`5c499ad0`)
    deleted `scriptBackend`. runit, s6 and OpenRC now run through the library's
    `service.NewSupervised`, whose `Stop`, `Start` and `WaitHealthy` honour their context and
    return the library's sentinel errors. The PLAN's U7 is not run.
* **D21 — The Windows CI script stamps `buildinfo` (REPORT A7).** `ci-windows-local.ps1:132` takes
  the `Makefile`'s `-X …/buildinfo.version=` and `….kind=local` pair.
* **D22 — mcrelay tells systemd it is ready (REPORT A8; question 10).** `Type=notify` in its unit
  template, `READY=1` and `STOPPING=1` at its ready and shutdown points, through the same kept
  socket 0210 D10 gives the daemon.
  * *Added 2026-10-26, the PLAN's U8 Deviation 1, the owner's answer "fix the mcrelay gap":* the
    unit also gets `TimeoutStartSec=240`, as mcremote's has (0054 amendment S8). mcrelay can spend up
    to `certs.ACMEStartupTimeout`, 3 minutes, issuing a certificate before it listens, and the
    manager's default start timeout is 90 s. The kept socket moves to `internal/sdnotify`, which
    both daemons import, because the library's `systemd.Notify` reads the environment on every call.
* **D23 — The Windows handoff is unblocked (0210 D3; question 8).** `v1.19.0` ships what 0210 D3
  waits on. Adopting it removes the Windows gate in `detacherFor`, adds Windows to the daemon's
  start-up `CleanupHandOffs`, and runs the agent-run update live on the Windows host. 0210 A1's
  refusal then stops applying on Windows by itself, since `insiderFor` returns false for a
  `Detacher`. Magic's task registers with `InteractiveToken`, the one logon type that hands off by
  default. A console window shows for about 50 ms when the hop starts (migration guide §19).
* **D24 — `acp-go-sdk2` stays on `v1.20.0`;** it is the newest tag, and nothing after it matters
  (above). D12 holds again with gobble.
  * *Deferred 2026-10-27, with D25 and D26:* `v1.21.0` to `v1.24.0` have shipped since. A later
    amendment assesses them first (the note under the Decision Outcome).
* **D25 — Per-turn token usage from `PromptResponse.Usage` (H1).** At a turn's end, Magic maps the
  standard usage to `event.Usage`, unless the turn already reported xAI usage, so grok is never
  counted twice. A shape-pin test holds the unstable field, as the plan tests do. The accounting
  rules of 0189 and 0199 are unchanged. `xaiusage.go`'s comment is corrected. Confirming grok's
  behaviour needs one live capture, which spends quota (question 11).
* **D26 — The SDK's conformance and helpers (H2 to H5).** `ReadTextFile` returns
  `NewResourceNotFound` for a missing file. The SDK's constructors replace the hand-built values of
  H3. `DroppedTapBytes` is logged when a capture closes, and a `ProtocolVersionError` at
  `initialize` gets its own message. H5's dead branches go, with a test that the SDK drops those
  updates.
* **D27 — Record corrections.** A dated note on 0195; a dated note on 0167-REPORT L3 and L4; G6
  and the Consequences here noted as no longer true.
  * *Decided and carried out 2026-10-27,* records only, as the SDK-gaps plan's C4 alone. gobble's
    pin was re-read for it: `v1.20.0` at `6ee20140`.
* **D28 — The product features the SDK enables are not decided here** (the list above). Each takes
  its own MADR when the owner wants it (question 12).
* **D9, re-checked at `v1.19.0`.** `archive`, `service/scm`, `verify/signednote` and
  `verify/ghattest` stay unused, for the same reasons. `Options.HopDetach` and `HandOffDir` keep
  their defaults. `Checker` and `CheckStore` stay new scope (REPORT A9).

### Consequences of the amendment

* Good, because Magic's Windows service path loses `schtasks.exe` for everything but enabling, and
  PowerShell entirely, along with about 200 lines of XML encoding and comparison. The library tests
  that code against captures of Magic's own task shape.
* Good, because an agent-run update can hand off on Windows too (D23), the last OS but the script
  supervisors.
* Good, because a binary that cannot run here is refused before the service stops (D16), and a
  stalled GitHub request fails in 30 s rather than 15 minutes.
* Good, because every start is confirmed one way, with the product's own probe (D4).
* Good, because non-grok agents' turns carry a token split (D25).
* Neutral, because `acp-go-sdk2`'s pin does not move.
* Bad, because D17, D4 and D23 change Windows behaviour the live runs exercise. Each needs the
  Windows test host, with the real task backed up and restored as 0210 R1 Deviation 3 set out.
* Bad, because D18 keeps one `schtasks.exe` call until the library ships an enable.
* Bad, because the library's definition comparison is stricter than Magic's. A task someone edited
  by hand in a setting setup does not render now reads as different, and `setup-service` asks for
  `--force` where it did not before.
* Bad, because D25 rests on an unstable schema field.

### Confirmation of the amendment

* `go.mod` names `go-selfupdate-lib v1.19.0`; `ci.yml`'s two pins name `a6af9dc7…`;
  `TestWorkflowPinsMatchGoMod` passes.
* `git grep` finds no `schtasks /create`, `/query`, `/delete`, no `powershell.exe` and no
  `encodeTaskXML` under `internal/cli/service`, and `launchctl print` only where a hint is printed.
* Fail-first tests, each seen failing on a scratch copy: the version probe refuses an unrunnable
  staged binary before any stop; the definition comparison names a differing path; a missing file
  answers -32002; a turn's standard usage reaches `event.Usage` once, and not beside xAI usage.
* On the Windows host: `make ci-windows` and `make ci-windows-smoke` pass (D21);
  `setup-service` install, `--force` and `--remove` against the real task, backed up and restored;
  `--remove` drains, exit 0 as in S9d; an agent-run `update --yes` hands off (D23).
* On the Linux host, `systemctl --user start` of mcrelay returns after `READY=1` (D22).

### Questions this amendment adds

These continue the numbering above.

* **7. Where the work lives.** The proposal is two new 0207 PLANs: one for `go-selfupdate-lib
  v1.19.0` (D15 to D22, D4), one for `acp-go-sdk2` (D25 to D27). Or a single PLAN?
* **8. The Windows handoff (D23).** It is 0210's D3, and 0210 said an amendment of 0210 adopts it
  when #6 ships. The proposal is a 0210 amendment B, taken before 0210's L2, so L2 checks the
  handoff on Windows once instead of the refusal. And the order: D15 and D21 first, then 0210's
  A2 and A3, then 0210 B, then L2, then the rest of this amendment?
* **9. The enable (D18).** File the `go-selfupdate-lib` issue for an exported enable and disable,
  and keep `schtasks /change` until it ships? Or keep it as Magic's for good?
* **10. mcrelay readiness (D22).** In scope, or left as is?
* **11. Quota.** D25 needs one capture-enabled `make live-grok`. The same run would close 0184's
  last item, the wire captures. Run it once, after D25 lands?
* **12. The product features (D28).** Open a MADR for any now? The proposal is none yet, with
  `ResumeSession` first when one is wanted.

*Answered 2026-10-23,* questions 8 to 11:

* **8.** "0210 amend B, before L2 (Recommended)". D23 is decided in a 0210 amendment B. The order
  is D15 and D21; 0210's A2 and A3; 0210 B; 0210's L2, which checks the Windows handoff; then the
  rest of this amendment.
* **9.** "File library issue (Recommended)". D18 stands: the issue is filed as the
  `go-selfupdate-lib` PLAN's first step, and `schtasks /change` stays until a release has it.
* **10.** "In scope (Recommended)". D22 stands.
* **11.** "Once, after D25 (Recommended)". One capture-enabled `make live-grok` after D25 lands,
  run on the owner's word for that run. It also closes 0184's wire-capture item.
  *Done: run 2026-10-28 against grok 1.0.70, with 105 tests passed. No prompt answer carried the
  standard `usage`, so none was counted twice. 0184's item closed 2026-10-29, with delete and
  resume confirmed by tests (the 0184 MADR's amendment of that date).*

Questions 7 and 12 were not put separately. Their proposals are what the PLANs carry: two
PLANs, and no product-feature MADR yet. Either can still be changed at the plans' review.
*Question 12 answered 2026-10-27: "None yet (Recommended)".*

### Evidence index of the amendment

* `go-selfupdate-lib` `v1.18.0` and `v1.19.0` from the Go module proxy (sums and origins above);
  its CHANGELOG and migration guide §18, §19; `compare/v1.17.2...v1.19.0`.
* `acp-go-sdk2 v1.20.0` from the module cache; `compare/v1.20.0...HEAD`; gobble-cli's `go.mod`
  from the contents API.
* The capability inventory of `acp-go-sdk2` was made by a read-only subagent, and its three load-
  bearing claims re-checked here: `PromptResponse.Usage` exists and is unstable; Magic reads only
  `StopReason`; gobble pins `v1.20.0`. Its other line citations were not each re-read.
* The scratch probe `m207b/pin_probe.py` and its logs, as quoted.

### Related records of the amendment

* [0210-MADR-hand-off-agent-run-updates-outside-the-service.md](0210-MADR-hand-off-agent-run-updates-outside-the-service.md):
  D3, which D23 unblocks, and D8, whose Windows refusal D23 makes moot.
* [0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md)
  and [0199-MADR-total-grok-per-turn-costs-into-the-session-cost.md](0199-MADR-total-grok-per-turn-costs-into-the-session-cost.md):
  the accounting D25 keeps.
* [0159-MADR-the-windows-service-path-is-half-wired.md](0159-MADR-the-windows-service-path-is-half-wired.md):
  D9 and D14, the PowerShell probe and the field comparison D17 replaces.
* [0125-MADR-launchd-bootout-is-asynchronous.md](0125-MADR-launchd-bootout-is-asynchronous.md): the
  launchd wait D19 and D4 move onto the library.

## Amendment — 2026-10-24 (C): `go-selfupdate-lib v1.21.0`

~~**Status of this amendment: proposed.** It becomes decided when the owner approves its PLAN step,
U1b of [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md).~~
**Decided 2026-10-24,** when the owner approved U1b ("U1b, then write 0212 PLAN"), with D29 carried
out as amendment E's D37. *Status line corrected 2026-10-25, when U4 began: it had been left
"proposed".*

**Why now.** [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)
D8, the owner's answer of 2026-10-24: "v1.21.0 first, under 0207". Two consequences:

* 0212's later bump, to the release that ships the supervised adapter, carries only the adapter.
* This record's remaining steps, U3 to U8, are written against the library they will build on.

The owner's ask, verbatim: "write the 0207 amendment for v1.21.0 first".

### Releases [observed 2026-10-24, the Go module proxy, `git ls-remote` and a local clone of the library]

| Release | Commit | Issue | What it is |
| :--- | :--- | :--- | :--- |
| `v1.20.0` | `ca1c0e227e3ca59fb59a46e64d8c934f47d48698` | [#7](https://github.com/maccavelli/go-selfupdate-lib/issues/7), closed 2026-10-24 | Handoffs no longer pass on the service manager's per-unit variables |
| `v1.21.0` | `29337d004689272c3f347db4c444ed81e0bec21c` | [#8](https://github.com/maccavelli/go-selfupdate-lib/issues/8), closed 2026-10-24 | `taskscheduler.Task.Enable` and `Disable`, filed by this record's D18 |

* **`v1.21.0` is the newest tag.** `go mod download -json …@v1.21.0` gives `"Sum":
  "h1:NDp8+ZRX0DbyeEGwSn/FeU/Kyv+xMtody2XWqq8g9so="` and origin `29337d00…`, ref
  `refs/tags/v1.21.0`.
* **Nothing changes in the module or the workflows.** `git diff v1.19.0 v1.21.0 -- .github go.mod
  go.sum` is empty. The library still asks for `go 1.26.10`, `x/mod v0.40.0`, `x/sys v0.47.0` and
  `x/term v0.43.0`. So `ci.yml`'s two pins move by commit and comment only, as in D15.
* **Both releases are additions only,** by the library's `make apicheck` (its CHANGELOG). One
  function returns less than before: `DiffDefinitions`, below.

### What `v1.20.0` and `v1.21.0` change, against Magic's code [read at 793b8f5b]

| Release | Change (CHANGELOG; migration guide §20, §21) | Magic today |
| :--- | :--- | :--- |
| `v1.20.0` | The systemd, launchd, SCM and Task Scheduler handoffs, `DetachProcess` and `ExecReconciler` leave out the variables `service.ManagerEnvNames` lists: systemd's per-unit run, notify, listen, watchdog, journal and trigger variables, `RUNTIME_DIRECTORY`, `CREDENTIALS_DIRECTORY`, and launchd's `XPC_SERVICE_NAME` and `XPC_FLAGS` (`selfupdate/service/managerenv.go:16-41`) | Magic's handoff passes no `HandOff.Env` (`internal/updateclient/client.go:328`), and its reconciler no `ExecOptions.Env` (`managed.go:79-85`). Nothing in `internal` or `cmd` reads any listed variable (`grep`, no match). `XDG_RUNTIME_DIR`, which the refresh child needs (`withUserRuntimeEnv`), is not listed |
| `v1.20.0` | `service.ScrubManagerEnv`, for processes a program starts itself | The daemon unsets only `NOTIFY_SOCKET`, at start-up (`internal/daemon/daemon_notify.go:13-22`, 0210 D10). Agents and their terminals take the daemon's environment otherwise (`acpagent/terminal.go`, `codex/provider.go`, `httpagent/provider.go`), so they still see the unit's other variables, such as `INVOCATION_ID` and `JOURNAL_STREAM` [derived] |
| `v1.21.0` | `taskscheduler.Task.Enable` and `Disable`; `Status.Disabled` | `startWindows` runs `schtasks /change … /enable`, and `stopWindows` `/change … /disable` (`internal/cli/service/control_schtasks.go:62`, `:90`). These are the two calls D18 kept |
| `v1.21.0` | `DiffDefinitions`, and so `SameDefinition`, no longer compare `Settings/Enabled`, the task's switch | Magic's own comparison does compare it (`taskFields.Enabled`, `setup_schtasks.go:187`, `:299`). Its `--no-start` path keeps a stopped task stopped by registering a definition rendered with `<Enabled>false</Enabled>` (`setup_schtasks.go:27-38`, `withTaskDisabled`) |
| `v1.21.0` | `Task.Start` is unchanged: it enables only a task its own `Stop` disabled, and otherwise returns `ErrTaskDisabled`. The guide: "Call `Enable` first" | D4 as amended: "on Windows, Magic enables the task (D18), then calls `Task.Start`" |

**The guide's advice for a setup that ships a disabled task** (§21): "`DiffDefinitions` no longer
tells you when someone enabled it. Check `Status.Disabled`, and call `Disable` after `Register`,
which sets the switch from the definition." That applies to Magic's `--no-start` path once U4
moves it to `Register` and `SameDefinition` (D31).

### The pin move, proven on a scratch copy [2026-10-24]

On a `git archive` copy of `HEAD` `793b8f5b`, given its own git index, nothing in the tree written
(`m207c/pin_probe.py` in the session scratchpad):

```text
go: upgraded github.com/maccavelli/go-selfupdate-lib v1.19.0 => v1.21.0
go mod tidy: exit 0
go.mod/go.sum: 6 changed lines (the require line; the module's h1: and /go.mod lines)
ci.yml: both pins moved
go build ./...: exit 0
go vet ./... darwin/arm64, linux/amd64, windows/amd64: exit 0, each
go test -count=1 ./...: exit 0; 43 packages ok, 0 FAIL lines
TestWorkflowPinsMatchGoMod, TestReleaseJobsReadTheSpec: ok (internal/updateclient)
```

The module's `/go.mod` hash is unchanged (`h1:boXH5t3X…`), since its `go.mod` is.

**What the probe does not cover.** It ran no race detector, and nothing on the Linux or Windows
hosts. Those are U1b's gates.

### The decisions this amendment proposes

* **D29 — Pin `go-selfupdate-lib v1.21.0`.**
  * *Superseded before it was carried out, 2026-10-24: amendment E's D37 pins `v1.22.0`, which
    includes `v1.21.0`. D30 and D31 rest on `v1.21.0`'s behaviour and still hold.*
  * `go.mod` and `go.sum`, and both `ci.yml` pins, to
    `29337d004689272c3f347db4c444ed81e0bec21c` with the comment `# go-selfupdate-lib v1.21.0`.
  * No Go code changes. Proven above.
  * D15's `v1.19.0` pin, already carried out, stays on record as the step before.
* **D30 — D18 is fulfilled: no `schtasks /change`.**
  * `v1.21.0` ships what question 9's answer waited for.
  * D4's Windows start becomes `Task.Enable`, then `Task.Start`.
  * Its stop becomes `Task.Stop`, which already disables the task, so the watchdog trigger cannot
    restart it (library `lifecycle.go:213-226`). No separate `Disable` call is needed there.
  * The work lands in U6, which already replaces `startWindows` and `stopWindows`. U6's goal, and
    this plan's, become "no `schtasks.exe`".
  * The printed manual hint, "Stop: schtasks /change … /disable then schtasks /end"
    (`result_print.go:174`), is advice for a person, not a call, and stays.
* **D31 — U4's comparison takes `v1.21.0`'s rule.**
  * `SameDefinition` no longer sees the switch. So a stopped task with an unchanged definition
    reads as unchanged, and nothing is registered. Its switch stays off.
  * Whether the task is off is read from `Status.Disabled`, not from the stored XML
    (`taskDisabled`, `setup_schtasks.go:108-111`).
  * When a changed definition is registered over a stopped task with `--no-start`, it is still
    rendered with `<Enabled>false</Enabled>` (`withTaskDisabled`). `Register` sets the switch from
    the definition, so the task is never enabled, even for a moment.
  * The guide's other route, registering enabled and then calling `Disable`, is not taken. The
    task's repeating watchdog trigger (0159 D5) could start the daemon between the two calls
    [derived; the window is not measured].
  * U4 measures this path on the Windows host, with the real task backed up and restored (W1 to
    W4), as its step 4 already sets out.
* **D32 — The service manager's variables: no change in Magic.**
  * `v1.20.0` scrubs Magic's handoff and reconcile child with no code change.
  * The daemon's own removal of `NOTIFY_SOCKET` stays (0210 D10). The library's scrub covers only
    processes the library starts, and agents are Magic's.
  * Scrubbing agents' environments with `ScrubManagerEnv` is not taken now. Nothing in Magic reads
    these variables, and no agent misbehaving on them is recorded.
  * Whether a third-party agent CLI reads them is [unverified]. It is a change to what every agent
    sees, so it needs a record of its own if the owner wants it (question 13).

### Consequences of the amendment

* Good, because Magic's last two `schtasks.exe` calls of the service path have a library
  replacement. Together with U4 and U6, Windows setup runs no `schtasks.exe`.
* Good, because a handed-off update and the reconcile child no longer inherit the service's
  per-unit variables, with no code change in Magic.
* Good, because a stopped task's switch no longer reads as a changed definition in U4's
  comparison. `setup-service` on a stopped task then asks for no `--force` for that alone.
* Good, because 0212's later bump carries only the adapter.
* Neutral, because D29 is one line in `go.mod`, two in `go.sum` and two pins.
* Bad, because U4 and U6 now depend on `v1.21.0`'s behaviour, so D29 must land before them.
* Bad, because D31 changes where a stopped task is detected, from the stored XML to
  `Status.Disabled`. The Windows host run must show that it reads the same.

### Confirmation of the amendment

* `go list -m github.com/maccavelli/go-selfupdate-lib` gives `v1.21.0`; `ci.yml`'s two pins name
  `29337d00…`; `TestWorkflowPinsMatchGoMod` passes.
* U1b's gates, including `go test -race ./...` on the Linux test host.
* After U6: `git grep -n '"/change"' -- internal/cli/service` finds nothing outside tests.
* U4's Windows run: an install with `--no-start` over a stopped task leaves it disabled, and reads
  as unchanged.

### Questions this amendment adds

These continue the numbering above.

* **13. Agents and the service manager's variables (D32).** Leave agents' environments as they are
  (the proposal), or open a record to scrub them with `ScrubManagerEnv`?

  *Answered 2026-10-24: "Leave agents as-is (Recommended)".* D32 is decided: no change in Magic.

### Evidence index of the amendment

* `go-selfupdate-lib` `v1.20.0` and `v1.21.0`: the CHANGELOG, migration guide §20 and §21, and
  `selfupdate/service/managerenv.go` and `taskscheduler/lifecycle.go`, read in a local clone at
  `v1.21.0`. `git diff --stat v1.19.0 v1.21.0` in that clone. The Go module proxy, for the sum and
  origin above.
* Issues [#7](https://github.com/maccavelli/go-selfupdate-lib/issues/7) and
  [#8](https://github.com/maccavelli/go-selfupdate-lib/issues/8), both closed 2026-10-24.
* This repository at `793b8f5b`: `internal/updateclient/client.go` and `managed.go`;
  `internal/cli/service/control_schtasks.go`, `setup_schtasks.go` and `result_print.go`;
  `internal/daemon/daemon_notify.go`.
* The scratch probe `m207c/pin_probe.py` and its logs, as quoted.

## Amendment — 2026-10-24 (D): replace the binary before the stop

~~**Status of this amendment: proposed,** with its scope decided by the owner (below). It becomes
decided when the owner approves its PLAN step, U3b of
[0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md).~~
**Decided 2026-10-25:** the owner approved U3b ("committed, proceed", after U3 was committed as
`17ccfd21`). D33 to D36 stand as written below. **Confirmed 2026-10-25:** U3b is complete. Its tests,
its guards and its live runs on every host are in the PLAN, with D36's one exception noted where it
stands.

**Why.**
[0212-REPORT-what-the-service-backends-can-take-from-go-selfupdate-lib.md](../reports/0212-REPORT-what-the-service-backends-can-take-from-go-selfupdate-lib.md)
B12 listed the library's replace-before-stop as considered, but not recommended alone. The owner
asked, verbatim, "scope and spec replace-before-stop", and answered four questions on 2026-10-24:

* **Scope:** "All backends, both (Recommended)";
* **The pre-stop check:** "Add PostInstall probe (Recommended)";
* **The record:** "0207 amendment D, U3b (Recommended)";
* **The guards:** "Static guards + live (Recommended)".

### Facts [read 2026-10-24]

**The library** (`go-selfupdate-lib`, since `v1.13.0`; its
`0020-MADR-precheck-gofmt-errors-and-replace-before-stop.md`, 4B):

* **What the option does.** `ManagedOptions.ReplaceBeforeStop`, through `NewManagedInstallerWith`,
  runs `Apply`, and with it `InstallOptions.PostInstall`'s probe, while a running service still
  runs. Then come `Stop`, `Reconcile`, `Start`, `WaitHealthy` and `Commit`, as before
  (`selfupdate/managed.go:29-39`, `:172-241`).
* **What a failure does.**
  * A failed `Apply` or probe rolls back and leaves the service running. Nothing restarts.
  * A failed `Stop` rolls the binary back too (`recoverStop`, `:287-300`): under a service that
    still runs, which is not restarted, or before a service that went down is started again.
* **Where it is reported.** `Result.ReplacedBeforeStop`, which the result document carries as
  `replaced_before_stop` (schema 4).
* **What the library measured for its record** (0020, "Item 4"):
  * **The window.** Stop-then-replace leaves the service down for the swap, a median of 24 to
    165 ms for a 16 MiB binary across macOS, Linux and Windows. That is before any post-install
    probe or reconcile. Stopping and starting dominate, and the order does not change them.
  * **A rename under a running binary works** on all three OSes. The old process keeps running,
    and a child it spawns from `os.Executable()` runs the **new** version.
* **The hazards the library names** for the option:
  * the service starting its own executable while it runs;
  * a stop hook that runs the program;
  * an unplanned restart in the gap, which starts the new binary before its health check.

  The planned stop, start and health check follow anyway.
* **The probe.** `NewVersionProber(args, want, timeout)` runs a binary with `args`, and requires
  exit 0 within the timeout, with stdout containing the tag (`want` nil) (`selfupdate/probe.go:95-107`).
  `InstallOptions.PostInstall` takes such a `Prober` and runs it on the installed path, before the
  commit (`types.go:648`; the guide, "Probe the new binary").

**Magic:**

* **The managed installer** is `selfupdate.NewManagedInstaller` (`internal/updateclient/managed.go:66`),
  the default order.
* **`InstallOptions`** sets only `TargetPolicy` in tests (`client.go:99-105`). There is no
  `PostInstall`.
* **0207 U3 (D16)** adds `Config.Probes`, the same version prober on the *staged* file, before
  anything is replaced. It does not run the binary at its installed path.
* **The release spec's identity arguments** are `["version"]` for both products (`spec_test.go:39`).
* **The first hazard: Magic never runs its own executable while serving.**
  * `os.Executable()` is called in non-test code only by setup-service and doctor
    (`internal/cli/doctor.go:64`; `internal/cli/service/setup.go:946`, `refresh.go:593`,
    `uninstall.go:54`).
  * `os.Args[0]` appears only in tests.
  * The Codex identity check runs the Codex binary (`internal/provider/codex/provider.go:1133-1142`).
  * The library's own read of Magic found the same: "No program found re-executes itself as a
    worker while it serves" (its 0020, "Item 4").
* **The second hazard: no definition has a stop hook that runs the program.**
  * Neither unit template has `ExecStop`, `ExecStopPost` or `ExecReload` (`grep`, no match).
  * The OpenRC script's `stop()` runs `start-stop-daemon --stop` (`supervisor.go:182`).
  * runit and s6 get no `finish` script.
* **The third hazard: an unplanned restart is possible on every backend.**
  * The daemon restarts on systemd (`Restart=always`), launchd (`KeepAlive`), runit and s6.
  * On Windows, the watchdog trigger stays enabled until `Stop` disables the task.
* **Magic already shows the result schema.** The handoff result Magic shows already carries
  schema 4 (`"schema_version":4` in the 0210 PLAN's W3 transcript). So the field needs no work in
  Magic.

### The decisions this amendment proposes

* **D33 — Replace before the stop, for both products and every backend.**
  * `managedInstaller` builds `selfupdate.NewManagedInstallerWith(standalone, backend, rec,
    selfupdate.ManagedOptions{ReplaceBeforeStop: true})`.
  * This covers systemd, launchd, Task Scheduler, runit, s6 and OpenRC, mcremote and mcrelay, and
    the handoff's detached run, which drives the same managed install.
  * A stopped or uninstalled service runs as today, since there is nothing to stop.
* **D34 — A post-install probe.**
  * `InstallOptions.PostInstall` is `NewVersionProber(<the spec's identity args>, nil, 30 s)`, run
    on the installed path.
  * It is built once, beside 0207 U3's `Config.Probes`, and both use it. So the staged file and
    the installed binary are each checked before the stop.
  * Its 30 s is the library's identity timeout for its own release checks. The PLAN measures the
    real `version` run on each OS before it fixes the value.
* **D35 — The hazards are held by two static guards,** each seen failing first.
  * **G1, own executable.** A test parses every non-test `.go` file under `cmd` and `internal`. It
    fails on:
    * a call of `os.Executable`, or a use of `os.Args[0]`;
    * a `procutil.Command` whose program is the literal `"mcremote"` or `"mcrelay"`.

    The only exceptions are an allowlist naming each file with the reason it never runs while
    serving: today's four setup-service and doctor sites. An allowlist entry that no longer
    matches also fails, as `internal/procutil/spawnsites_test.go` does for its own allowlist.
  * **G2, definitions.** A test renders every definition setup-service writes, and asserts that the
    product binary's path appears exactly once, as the start command:
    * both units;
    * the LaunchAgent plist;
    * the task XML;
    * the runit, s6 and OpenRC definitions, in every form 0212 renders.

    It also asserts that no unit has `ExecStop`, `ExecStopPost` or `ExecReload`, and that no script
    supervisor gets a `finish` script that runs the binary.
* **D36 — The proof.**
  * **Tests, over the fake GitHub server and a fake lifecycle,** in the existing
    `managed_test.go` pattern:
    * the target holds the new release's bytes when `Stop` is called;
    * a release whose binary fails `version` ends the update with an error, no `Stop` called,
      and the target unchanged;
      * *As built (0207 PLAN, U3b Deviation 1):* the binary fails only at its installed path.
        U3's staged probe already refuses one that fails while staged, so only this case shows
        `PostInstall` at work.
    * the `--json` result carries `replaced_before_stop: true`.
  * **Live success runs, on the owner's word:** an update from a shell, and an agent-run update
    that hands off. They run on each host:
    * Linux (systemd) and macOS (launchd), on the scratch services;
    * runit, s6 and OpenRC, in the three containers;
    * Windows, on the real task by W1 to W4.

    Each result must have `replaced_before_stop: true`, `service_started: true` and
    `rolled_back: false`, with a new daemon PID and `/healthz` up.
    * *Note, 2026-10-25 (0207 PLAN, U3b Deviation 2; the owner chose the first resolution):*
      under runit, s6 and OpenRC, an agent-run update refuses before it downloads, because they
      have no handoff yet (0210-MADR D8). There, the success run is the update from a shell, with
      L-b. The agent-run success run for those three moves to
      [0212-PLAN-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-PLAN-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)
      P6, whose adapter brings the handoff.
  * **A live failing probe:** a live-tagged Go test, `TestLiveReplaceFirstProbeFailure`, behind
    `MCREMOTE_REQUIRE_REPLACEFIRST=1`.
    * It builds the updater as `New` does, against the fake GitHub server, serving a release
      whose binary fails `version`.
    * It runs against the real backend of the host's scratch service.
    * It must end with the target's bytes unchanged, and the daemon's PID and start token
      (`procutil.ProcessStartToken`) unchanged: the service was never stopped.
    * It runs where the success runs do, the Windows real task included, by W1 to W4.

    Product code gains no switch for it: `Options.APIBaseURL` and `TargetPath` are already
    test-only.

### Consequences of the amendment

* Good, because a release that cannot run at its installed path is refused with the daemon never
  stopped. Without the option, the same failure is caught only after the stop, and recovered by a
  restart.
* Good, because the swap leaves the downtime window: 24 to 165 ms, by the library's measure, plus
  the probe.
* Good, because the premise is held by tests. A later change that makes the daemon run its own
  binary, or adds a stop hook, fails G1 or G2 before it can make the option unsafe.
* Neutral, because stopping and starting, which dominate the downtime, are unchanged.
* Bad, because an unplanned restart between the swap and the stop starts the new binary before its
  health check. On Windows the watchdog trigger can do that within a minute, if the daemon exits
  in that gap. The planned stop, start and health check then follow, as the library documents.
* Bad, because until 0212's D11 raises the script backend's stop bound, a stop that times out on a
  slow drain rolls the new binary back under the running daemon. The update fails safe, and the
  daemon keeps running.
* Bad, because the live failing-probe test touches the Windows host's real binary. It runs only by
  W1 to W4, with the binary and config backed up and restored.

### Confirmation of the amendment

* `managedInstaller` passes `ReplaceBeforeStop: true`, and `New` sets `PostInstall`. U3b's tests
  pass, each seen failing on a scratch copy:
  * with the default order, the "target at Stop" test fails;
  * without `PostInstall`, the failing-probe test sees a `Stop`.
* G1 fails on a planted `os.Executable()` call in an `internal/daemon` file, and on a stale
  allowlist entry. G2 fails on a planted `ExecStop=` in a unit template copy. Both plants are in a
  scratch copy.
* The live runs of D36 are recorded in the PLAN, per host.

### Questions this amendment adds

None. The owner's four answers settle its scope. The PLAN's U3b goes to the owner for approval.

### Evidence index of the amendment

* `go-selfupdate-lib`, read at `fff0940`: `selfupdate/managed.go`, `probe.go` and `types.go`; its
  `0020-MADR-precheck-gofmt-errors-and-replace-before-stop.md`; and
  `docs/guides/extending-selfupdate.md`, "Replace before the stop" and "Probe the new binary".
* This repository at `793b8f5b`:
  * `internal/updateclient/managed.go`, `client.go`, `spec_test.go` and `managed_test.go`;
  * the unit templates;
  * `internal/cli/service/supervisor.go`;
  * the `os.Executable` and `os.Args[0]` searches above;
  * `internal/procutil/spawnsites_test.go`, the guard pattern G1 follows.

## Amendment — 2026-10-24 (E): the newest releases of all three libraries

**Status of this amendment: proposed.** It becomes decided when the owner approves its PLAN step,
U1b of [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md),
now at `v1.22.0`.

**The owner's ask, verbatim:** "assess the latest releases of go-selfupdate-lib, acp-go-sdk2,
go-llmprovider-sdk, and ensure we are pinning latest. assess the impact of the new releases on our
planning."

### Releases [observed 2026-10-24, `git ls-remote --tags`, the Go module proxy, the GitHub compare API and local clones]

| Library | Magic pins | Newest | Newest's commit | What is new for Magic |
| :--- | :--- | :--- | :--- | :--- |
| `go-selfupdate-lib` | `v1.19.0` | **`v1.22.0`**, 2026-10-24, tag only | `84805563ed252df384f59ced45a3daf529702ab4` (tag object `b7c2181…`) | `v1.20.0` and `v1.21.0`, as amendment C; and the supervised adapter |
| `acp-go-sdk2` | `v1.20.0` | `v1.20.0`, unchanged | `4fa720471d44bdcbe3f7318d494d95035708fe12` | Nothing |
| `go-llmprovider-sdk` | not a dependency | `v1.22.2`, 2026-10-24 | `97cc0b2f…` (tag object `88908c26…`) | Nothing: 0185 keeps it out of `go.mod` |

* **`go-selfupdate-lib v1.22.0`** adds `service.NewSupervised`, `Supervised`, `SupervisedOptions`,
  `SupervisedState` and `ErrNoSupervisor`, from library issue #2 (its CHANGELOG; migration guide
  §22). `make apicheck` reports it compatible with `v1.21.0`.
  * Outside the documents, `git diff --stat v1.21.0 v1.22.0` touches only the adapter's files,
    a new `internal/procfs`, `errors.go`, `doc.go`, one `//nolint:gosec` comment in
    `sameexec.go`, and the library's own `ci.yml` and `.golangci.yml`.
  * `git diff v1.19.0 v1.22.0` over `build-selfupdate-release.yml`,
    `publish-selfupdate-release.yml`, `go.mod` and `go.sum` is empty. So `ci.yml`'s two pins move
    by commit and comment only, as in D15 and D29.
  * The files amendment D cites (`managed.go`, `probe.go`, `types.go`) are unchanged since
    `fff0940`, the commit it read.
  * `go mod download -json …@v1.22.0` gives `"Sum": "h1:AHBwXsYs08WGTteAU7e/1NAa/CqbnTbZx+Xj6LSjKmk="`,
    origin `84805563…`, ref `refs/tags/v1.22.0`. The `/go.mod` hash is unchanged.
  * What the adapter means for runit, s6 and OpenRC is
    [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md)'s
    amendment B.
* **`acp-go-sdk2`.** `compare/v1.20.0...HEAD` is 26 commits and 31 files, the same as amendment B
  found on 2026-10-23. The only Go file is `agentproc/tap_test.go`, a test. Nothing is unreleased
  that matters to Magic.
* **`go-llmprovider-sdk`,** read in a local clone, `v1.21.0..v1.22.2`:
  * `v1.22.0`: the wizard's `ShowDeviceCode` hook, `NoPastePrompt`, and a paste prompt that ends
    when the browser's loopback wins (its 0057-MADR);
  * `v1.22.1`: wizard fixes and test cache isolation (its 0058-MADR and 0059-MADR);
  * `v1.22.2`: the Claude key refusal hint names only the key prefix.

  Each was asked for by gobble-cli. Its unreleased 0061-MADR answers gobble's issue #9. None
  touches what Magic does.
* **gobble-cli's `go.mod`** pins `acp-go-sdk2 v1.20.0`, `go-llmprovider-sdk v1.22.2` and
  `go-selfupdate-lib v1.22.0`. So 0184's one-pin rule for `acp-go-sdk2` still holds.

### The pin move, proven on a scratch copy [2026-10-24]

The `m207c` probe, with its target moved to `v1.22.0` (`m207e/pin_probe.py` in the session
scratchpad). It ran on a `git archive` copy of `HEAD` `793b8f5b`, given its own git index, with
nothing in the tree written:

```text
go: upgraded github.com/maccavelli/go-selfupdate-lib v1.19.0 => v1.22.0
go mod tidy: exit 0
go.mod/go.sum: 6 changed lines (the require line; the module's h1: and /go.mod lines)
ci.yml: both pins moved
go build ./...: exit 0
go vet ./... darwin/arm64, linux/amd64, windows/amd64: exit 0, each
go test -count=1 ./...: exit 0; 43 packages ok, 0 FAIL lines
-run TestWorkflowPinsMatchGoMod|TestReleaseJobsReadTheSpec: exit 0
```

**What the probe does not cover.** As in amendment C: no race detector, and nothing on the Linux
or Windows hosts. Those are U1b's gates.

### The decisions this amendment proposes

* **D37 — Pin `go-selfupdate-lib v1.22.0`, in place of D29's `v1.21.0`.**
  * `go.mod` and `go.sum`, and both `ci.yml` pins, to
    `84805563ed252df384f59ced45a3daf529702ab4` with the comment `# go-selfupdate-lib v1.22.0`.
  * No Go code changes. Proven above.
  * U1b does it, in D29's place. D30 and D31 rest on `v1.21.0`'s behaviour, which `v1.22.0`
    includes, so they stand.
  * The adapter arrives unused. 0212's stage 2 is the first caller, and its own bump step lapses
    (0212 amendment B, E2).
* **D38 — `acp-go-sdk2` stays on `v1.20.0`.** It is still the newest tag (D24, re-checked).
* **D39 — `go-llmprovider-sdk` stays out of `go.mod`.** 0185 stands, as D7 says. Its new releases
  reach Magic only through gobble, which already pins `v1.22.2`
  ([0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md](../reports/0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md),
  its first feasible route).

### Consequences of the amendment

* Good, because one pin step brings Magic to the newest release of the library, the one 0212
  builds on.
* Good, because 0212's stage 2 no longer has a pin move of its own.
* Neutral, because `v1.22.0` adds code that Magic does not call until 0212's stage 2.
* Neutral, because `acp-go-sdk2` and `go-llmprovider-sdk` change nothing here.
* Bad, because amendment C's "0212's later bump carries only the adapter" lapses. A regression from
  the adapter's arrival would show at U1b, not at 0212's stage 2. Nothing Magic calls changes in
  `v1.22.0`, so none is expected.

### Confirmation of the amendment

* `go list -m github.com/maccavelli/go-selfupdate-lib` gives `v1.22.0`. `ci.yml`'s two pins name
  `84805563…`, and `TestWorkflowPinsMatchGoMod` passes.
* U1b's gates, as amendment C set them, including `go test -race ./...` on the Linux test host and
  `make ci-windows` on the Windows host.
* `go list -m github.com/maccavelli/acp-go-sdk2` still gives `v1.20.0`. `go list -m all` names no
  `go-llmprovider-sdk`.

### Questions this amendment adds

None. The owner's ask sets the pin. U1b, at `v1.22.0`, goes to the owner for approval.

*Approved 2026-10-24:* the owner answered "U1b, then write 0212 PLAN". D37 is carried out by U1b;
its execution record is in the PLAN.

### Evidence index of the amendment

* `git ls-remote --tags` of all three libraries, 2026-10-24.
* `go-selfupdate-lib`: `CHANGELOG.md` and migration guide §22 at `v1.22.0`; its 0035-PLAN execution
  record, M8; `git diff --stat` in a local clone, as quoted. The Go module proxy, for the sum.
* `acp-go-sdk2`: `gh api repos/maccavelli/acp-go-sdk2/compare/v1.20.0...HEAD`.
* `go-llmprovider-sdk`: `git log v1.21.0..v1.22.2` in a local clone, and its 0057, 0058, 0059 and
  0061 MADRs.
* gobble-cli's `go.mod`, in a local clone.
* The scratch probe `m207e/pin_probe.py` and its logs, as quoted.

## Amendment — 2026-10-27 (F): what setup-service needs from the backends, as U6's first step measured it

*Decided by the owner, 2026-10-27,* in U6's Deviation 1 (the PLAN's execution record). The four
answers were each the recommended one.

D4, as amended, says setup-service stops, starts and confirms a start through the backends, "with
the product's probe". U6's first step read each call site against go-selfupdate-lib `v1.22.1`, at
`f5d9af5a`. Four facts stand in the way of doing that inside U6's files:

* **F-1. setup-service has no product probe.** The probe and Windows' stop request come from each
  product's command layer (`internal/cli/health.go:24-30`, `internal/relay/health.go:29`), and
  only the update passes them. On Linux, `NewBackendWith` and the probe take the kind from the
  recorded backend, and detect it only when there is none (`backendrecord.go:51-57`). `Setup`
  detects its own kind and records it after the start, so a record an earlier setup left on another
  supervisor names the wrong kind for both.
* **F-2. The adapter knows the old instance only from its own `Stop`.** `Supervised.WaitHealthy`
  counts an instance other than the one `Stop` recorded (`supervised_lifecycle.go:199-206`). A
  restart that setup's own script commands made would leave it nothing to compare.
* **F-3. The backends' limits are not setup's.** Setup waits up to `MCREMOTE_SVC_WAIT_SECS`, 15 s by
  default. The backends need 10 s of one instance up, within a 60 s timeout
  (`service/poll.go:46-48`).
* **F-4. No library call restarts a loaded launchd job in place.** `Job.Start` kickstarts without
  `-k` (`launchd/lifecycle.go:248-279`), so the unchanged-plist path of
  [0125-MADR-launchd-bootout-is-asynchronous.md](0125-MADR-launchd-bootout-is-asynchronous.md) D4
  has no equivalent.

### The decisions this amendment makes

* **D40 — setup-service gets each product's health wiring, and builds its backend by kind.** The
  setup options carry the same `HealthOptions` the update passes. `Setup` builds the backend for
  the kind it detected, not the one recorded. So every start check runs the product's probe, and
  Windows `--remove` asks the daemon to stop and drains, as D4's consequences said.
* **D41 — setup starts the script kinds through the adapter.** Setup keeps what enables a service
  on these supervisors: the down file, `rc-update add`, OpenRC's runlevels and s6's rescan. A start
  is then the adapter's `Stop`, `Start` and `WaitHealthy`, as in the update and the installer hook.
  `settle` and its samplers go.
* **D42 — the start check takes the backends' limits.** A 10 s settle window and a 60 s timeout.
  `MCREMOTE_SVC_WAIT_SECS`, when set, becomes the timeout, and must be above 10, and above 45 on
  runit, as the update already requires (0212 D17). This replaces setup's default of 15 s, and its
  acceptance of any positive value.
* **D43 — on macOS, setup restarts with `Job.Stop`, then `Job.Start`.** It is one path for every
  case, with the bootout wait 0125 D1 asked for, in the library. `Job.Start` then retries
  `bootstrap`'s errors 5 and 37, but only after that wait has seen the job gone, which is not the
  retry 0125 D6 rejected. Its `enable` runs on every start, so `--no-enable` no longer skips it.

### Consequences of the amendment

* Good, because setup, the update and the installer hook confirm a start one way, with the
  product's probe, on all six kinds.
* Good, because Windows `--remove` drains the daemon instead of ending it.
* Good, because setup refuses to stop the service from inside it, on every backend.
* Neutral, because each kind keeps when setup restarts. systemd, launchd and the script kinds
  restart on every start. Windows restarts only with `--force`.
* Bad, because a healthy start now takes at least 10 s, against about 3 s before. A start that
  never settles reports `starting` after 60 s, not 15.
* Bad, because a `MCREMOTE_SVC_WAIT_SECS` of 10 or less, which setup accepted, is now refused.
* Bad, because the unchanged-plist restart on macOS goes through a bootout again, the window 0125
  D4 avoided. The library's wait and retry stand in for it, and only the owner's Mac can show
  whether they hold (0125 D7).

### Confirmation of the amendment

* U6's step 2 tests, and these as well:
  * `Setup` passes `Options.Health` to the backend, and a first install builds it by kind;
  * a script-kind start that restarts is stopped and started through one adapter;
  * the start check's timeout follows `MCREMOTE_SVC_WAIT_SECS`, and a value of 10 is refused;
  * an unchanged plist with a running job is stopped and started, with no `kickstart -k`.
* U6's step 4 host runs, with a `--force` on the macOS scratch service whose plist is unchanged.

*Note, 2026-10-27, from U6's Deviations 2 and 3, decided by the owner:*

* **D40 covers the relay set up beside mcremote** (`MCREMOTE_WITH_RELAY_SERVICE=1`). The relay
  exports its health wiring (`relay.SetupHealth`), and `SetupRelayBeside` takes it from mcremote's
  command, so that start is confirmed with the relay's `/healthz`, not mcremote's admin socket.
* **D42 has a Windows rule.** mcremote's task is given 30 s to drain, which must fit inside the
  timeout (`taskscheduler/task.go:149-150`). So on Windows, with mcremote's stop request, setup
  refuses a `MCREMOTE_SVC_WAIT_SECS` of 30 or less before anything is written.
* **D30 is met in full.** With `startWindows` and `stopWindows` gone, nothing called `runSchtasks`
  or `taskName`. Both are deleted, so no code path can run `schtasks.exe`.

## Amendment — 2026-10-27 (G): `go-selfupdate-lib v1.22.2`

*~~Proposed~~ decided 2026-10-27,* when the owner approved U1c ("Approve, proceed"), and carried
out with U6 (`59bcadad`). The owner asked, verbatim: "update the pin for go-selfupdate-lib to
current."

### Facts [observed 2026-10-27, `git ls-remote` and the GitHub compare and contents APIs]

* **G-1. `v1.22.2` is the newest tag.** It peels to `50deb3f1317bcd9142f2c91be2b5609957c677c9`,
  six commits after `v1.22.1` (`4d9ae417…`), which Magic pins (`go.mod:12`; `ci.yml:743`, `:764`).
  `main` is at `08e41cba`, past the tag; a pin follows tags.
* **G-2. What it changes** (its `CHANGELOG.md`, "v1.22.2 - 2026-10-27"; 21 files):
  * systemd's `Stop` runs `systemctl stop --no-block`, so its bound holds: under
    `TimeoutStopSec=infinity` a program that ignored TERM held `Stop` for as long as it ran. A stop
    that outlives the bound is `ErrTimeout`.
  * launchd's `Stop` runs `bootout` on a context the caller cannot end, so a cancellation during it
    no longer returns before the job has gone. A `bootstrap` its bound ends mid-run is
    `ErrTimeout`.
  * Task Scheduler's stop timeout always says `<path> still runs after it was stopped`.
  * `CommandError.Backend`'s documentation covers a program's own commands, as Magic's runit, s6
    and OpenRC commands use it (0212 P2).
* **G-3. Nothing Magic calls changes shape.** No exported API changed: the one non-test API file,
  `selfupdate/service/errors.go`, gains three comment lines. No file under `.github/workflows`
  changed, so the release workflows the two `ci.yml` pins name are unchanged.
* **G-4. `Supervised` is unchanged** since `v1.22.1`: no file of the adapter is in the diff. 0212's
  live tests ran on `v1.22.1`'s adapter, which is `v1.22.2`'s.
* **G-5. U6 gains from it.** setup's systemd and launchd stops go through these `Stop`s once U6
  lands (D4, D43), so its bounds and its launchd cancellation come with this pin.

### The decisions this amendment proposes

* **D44 — Pin `go-selfupdate-lib v1.22.2`.** `go.mod` requires it, and both `ci.yml` pins name
  `50deb3f1317bcd9142f2c91be2b5609957c677c9`. No source changes. The PLAN's step U1c carries it out.

### Consequences of the amendment

* Good, because the update's systemd stop is bounded under `TimeoutStopSec=infinity`, and its
  launchd stop is not cut short by a cancellation.
* Good, because U6 builds on the fixed stops rather than on the ones they replace.
* Neutral, because Magic's unit sets no `TimeoutStopSec`, so its stop already had systemd's finite
  default, 90 s. The systemd fix is a guard here.
* Bad, because U6's work is in the working tree, uncommitted and with its tests not yet compiling.
  The pin must land without it, or wait for it (the PLAN's U1c says how).

### Confirmation of the amendment

* `go list -m github.com/maccavelli/go-selfupdate-lib` gives `v1.22.2`. Both `ci.yml` pins name
  `50deb3f1…`, and `TestWorkflowPinsMatchGoMod` passes.
* U1b's gates, run on the commit as it will land.

## Amendment — 2026-10-28 (H): `acp-go-sdk2 v1.21.0` to `v1.25.0`, and D24 to D26 again

*~~Proposed~~ decided 2026-10-28* (the answers below). The owner asked, verbatim: "do 1 and 2.
note: acp-go-sdk2 has moved past 1.24". This is the assessment the Decision Outcome's deferral
calls for. It re-proposes the pin (D24), D25 and D26 on the newest release. It also proposes D45 to
D47, for what the five releases add.

### Facts [observed 2026-10-28: `git ls-remote --tags`, the GitHub releases API, the Go module proxy, and a scratch clone of `ac964b31`]

* **H-1. Five releases since the pin.** Each one's notes say `make api` found compatible
  additions only, and none changes what is sent or accepted on the wire unless a side opts in.
  The ACP schema is `1.25.0` in all five.

  | Tag | Commit | Published | What it adds |
  | :--- | :--- | :--- | :--- |
  | `v1.21.0` | `7f3b2100` | 2026-10-25 | constructors for every stable `SessionUpdate` variant; `SessionUpdate.Kind()`; `ClientRefusals` |
  | `v1.22.0` | `67c7a8eb` | 2026-10-25 | consumer helpers: `AllSessions`, `PickPermissionOption`, `ConfigOptionByCategory`, `SessionConfigSelectOptions.All`, `MetaObject`, `DecodeMeta`, `IsMethodNotFound`, `IsAuthRequired`, `IsCancelled`, `Kind()` on four unions, `RefusedMessages` |
  | `v1.23.0` | `9e0baace` | 2026-10-27 | the unstable boundary (H-3); `CompactionTimeline`; `ExtensionMux`; package `acptest` |
  | `v1.24.0` | `22009896` | 2026-10-27 | elicitation: `Fields`, `Check`, `CheckSchema`, `ParseValue`, `ParseElicitationURL`, `WithElicitationCheck` |
  | `v1.25.0` | `8505722c` | 2026-10-28 | package `clienthost` (`Terminals`, `Files`); `ToolCallTimeline`; `agentproc.LineTap`, `ExitedFirst`, `WaitContext`; `EndedCleanly`; `AllowsValue`; `IsResourceNotFound`; an `agentproc` close fix |

* **H-2. The pin move, proven on a scratch clone** (`leftovers/sdk_pin_probe.py` in the session
  scratchpad; the clone was removed):
  * `go get github.com/maccavelli/acp-go-sdk2@v1.25.0` moved that module alone; a second `tidy`
    changed nothing.
  * `go build ./...`, `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64,
    `./scripts/vulncheck.sh`, the workflow pin tests, and `go test -count=1 -race ./...`
    (44 packages ok) all passed.
  * **`make staticcheck` failed:** 58 SA1019 uses, each reported once for each of its three GOOS
    runs (174 lines), all in `internal/provider/acpagent`. 23 are outside tests:
    * `acpagent.go:1110`, the client's `Plan: &acp.PlanCapabilities{}` advertisement;
    * `plans.go:21-46`, the identified plans of
      [0192-MADR-show-an-agents-identified-plans-on-the-phone.md](0192-MADR-show-an-agents-identified-plans-on-the-phone.md);
    * `session.go:1960-1964`, their dispatch;
    * `session.go:2070-2075`, the subagent decline branches (H5 of amendment B).

    35 are in tests: `plans_test.go`, and `advertise_test.go:68`.
* **H-3. The deprecations are permanent, and have no replacement.** From `v1.23.0` every
  declaration `acp` took from the ACP *unstable* schema carries "Deprecated: this declaration comes
  from the unstable ACP schema; it stays under v1 for compatibility only". There are 202 such
  notices. The names are frozen and still work. Package `unstable` receives only what a *later*
  unstable schema adds; at schema `1.25.0` it holds documentation only. The release notes' advice
  is to suppress SA1019 for those uses. Magic's
  [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md)
  D5 says "No `staticcheck.conf`, no `//lint:ignore`". Its reason was that each SA1019 finding then
  had a direct replacement (its F9). These have none.
* **H-4. D25's field is among them.** `PromptResponse.Usage` is deprecated at `v1.25.0`
  (`types_gen.go:11385-11391`), and so is the `Usage` type. Its shape is unchanged since `v1.20.0`:
  input, output, total, cached-read, cached-write and thought tokens. Magic does not read it today,
  so it is not among H-2's 58. D25 would add uses.
* **H-5. Two behaviour faults the new helpers would fix,** each re-read at `ac964b31`:
  * **Auto-approval can pick a reject option.** `autoAllow` (`session.go:2593-2624`), used when
    `always_approve` or the session's auto mode is on (`:2422-2428`), and `pickAllowOptionID`
    (`:2628-2642`) both take the first option whose kind *or name* contains "allow" or "approve".
    An option named "Don't allow" of kind `reject_once` matches. With no match, the first option
    is taken, whatever its kind. `PickPermissionOption` (`helpers.go:634`) reads only the kind,
    and reports false when nothing matches.
  * **Grouped select values never reach the phone.** `emitConfigOptions` (`session.go:2288-2292`)
    reads only `Options.Ungrouped`. An agent that sends grouped values shows an empty list.
    `SessionConfigSelectOptions.All()` (`helpers.go:701`) flattens both. This one is already
    decided elsewhere:
    [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md)
    D3 forwards grouped selects *with their groups* and categories, in its PLAN's P7, not yet run
    (its N3 names this fault). So it is not part of D46; P7 can use `All()` or the groups.
* **H-6. Helpers that would replace code Magic wrote for itself,** with no change of behaviour
  unless noted. This is from an inventory by a read-only subagent; the rows marked ✓ were re-read
  here:

  | SDK | Magic today |
  | :--- | :--- |
  | `ConfigOptionByCategory` ✓ | `selectOption`, `session_meta.go:73-80` |
  | `MetaObject` | `metaObject`, `grok/fallback.go:138-145` |
  | `IsMethodNotFound`, `IsAuthRequired` | `sessioncaps.go:188-193`, `runtime.go:344-349`; the SDK's also covers a method the peer did not advertise |
  | `IsCancelled` | the first part of `isBenignPromptErr`, `session.go:2836-2858` |
  | `ContentBlock.Kind()`, `SessionUpdate.Kind()` | `blockKind`, `compaction.go:89-102`, and the hand decode at `session.go:2076-2081`. An unknown variant's decline names its discriminator, where Magic says "unknown" |
  | `AllSessions`, `SessionInfo.Updated` | the paging loop and `parseACPTime`, `sessioncaps.go:76-104`, `:207-216`. `AllSessions` catches any repeated cursor, not only the last; Magic's 20-page cap would move to the context |
  | `agentproc.LineTap` | `wirecap`'s `tapWriter`, `wirecap.go:181-209` |
  | `Process.ExitedFirst` | the 500 ms `exitPreference` timer, `session.go:1304`, `:1331-1339`. *Corrected 2026-10-28 (the SDK-gaps PLAN's C1b Deviation 1): not like for like. It is final only once `Wait` returns, after `agentproc` has stopped an agent whose connection ended first, so the exit watcher decides the message, and a lost connection is reported up to the 2 s grace later. Corrected again the same day (C1b Deviation 2): withdrawn. An agent's death closes its output, so the connection's end is usually seen first, and `ExitedFirst` is false for most real deaths: 25 of 30 runs of `TestSpawnedAgentDeathIsOneDisconnect` reported a killed agent as a lost connection. The timer stays; `ExitedFirst` moves to D47* |

* **H-7. Larger adoptions that change behaviour, each needing its own decision:**
  * `clienthost.Files` would serve `fs/*` and answer a missing file `-32002`. It refuses `line` 0,
    which Magic reads as the start, keeps a file's mode where Magic writes `0644`, and creates no
    directory where Magic always does.
  * `clienthost.Terminals`, about 330 lines of `terminal.go`. Magic keeps its `bash -lc` routing
    through a custom `Command`. An unknown terminal id would answer `-32002`, and output would be
    cut at a character boundary.
  * Elicitation's `Check`, against Magic's checker (`elicitation.go:302-392`). Magic trims
    whitespace, refuses `5.0` for an integer, and checks email with `net/mail`; `Check` does none
    of these.
  * `CompactionTimeline`, `ExtensionMux`, `ToolCallTimeline`, `AllowsValue`, `WaitContext`,
    `EndedCleanly`, `RefusedMessages`, and `acptest` in Magic's tests.
* **H-8. The `agentproc` close fix does not reach Magic's capture.** `v1.25.0`'s `Close` reads
  the agent's last output before closing. Magic stops the agent with `Kill` (`acpagent.go:528`),
  which closes at once in both versions, and never calls `Close`.
* **H-9. Amendment B's other facts, re-read at `ac964b31`:**
  * Magic still reads only `StopReason` (`session.go:537-543`).
  * `xaiusage.go:14-18` still says the standard `usage_update` has no cache fields. That is true of
    the `usage_update` (`v1.25.0` `types_gen.go:14896`), not of the unstable `Usage`. It cites
    `acp-go-sdk@v0.13.5`, a module Magic no longer uses.
  * The hand-built values D26 replaces have moved slightly: permission outcomes are at
    `session.go:2470-2483` and `:2610-2623`, and the second config request at `:1165-1180`. The
    rest hold where amendment B put them.
  * Every helper D26 names exists at `v1.25.0`, `NewResourceNotFound` at `errors.go:290`.
  * `grok/testdata/wire/` holds `1.0.13` and `1.0.50` only. *Since 2026-10-29 it also holds
    `1.0.70`, from the SDK-gaps plan's C3.*
* **H-10. gobble.** gobble-cli's `go.mod`, at its default branch's `6ee20140`, still pins
  `acp-go-sdk2 v1.20.0` (and `go-selfupdate-lib v1.17.0`). It lists staticcheck as a `tool`, so
  H-3 would reach it too.
  [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
  D12 has the two repositories pin the same tag and bump together.

### The decisions this amendment proposes

* **D24, superseded by D45.**
* **D45 — The pin, and the unstable names (question 13).** Proposed: pin `v1.25.0`, and amend 0170
  D5 with one narrow exception. A use of an `acp` declaration whose only deprecation is the
  unstable-schema notice may carry `//lint:ignore SA1019`, with its reason and the MADR that
  chose the feature. That covers 0192's plans, the plan and subagent capabilities, and D25's
  usage.
  * The scope stays narrow:
    * line by line in non-test code;
    * file-wide (`//lint:file-ignore`) only in `plans.go` and `plans_test.go`, whose whole subject
      is the unstable plan;
    * no `staticcheck.conf`, and no other check or deprecation suppressed.
  * A planted use of any other deprecated name in those files must still fail `make staticcheck`.
  * *Amended 2026-10-28, the SDK-gaps plan's C0 Deviation 1, decided by the owner ("File-wide +
    gate (Recommended)"):* a file-wide `//lint:file-ignore SA1019` hides *every* SA1019 in its
    file, so the bullet above could not hold by the directive alone. `make staticcheck` also runs
    `staticcheck -show-ignored -f json` and fails on any ignored finding that is not SA1019 with
    the unstable-schema notice on an `acp-go-sdk2` declaration, and on any directive whose reason
    does not name this amendment. That bounds every directive, line or file.
  * The alternatives:
    * **`v1.22.0`,** the newest tag without the notices. It has H-5's fixes and most of H-6. It
      lacks `LineTap`, `ExitedFirst` and everything in H-7, and stays one minor version from where
      a later bump meets H-3 anyway.
    * **Stay on `v1.20.0`,** and give up H-5's fixes.
* **D25, re-proposed at `v1.25.0`.** Unchanged in substance: at a turn's end, map
  `PromptResponse.Usage` to `event.Usage` unless the turn already reported xAI usage. Its uses take
  D45's exception. `xaiusage.go`'s comment is corrected (H-9).
* **D26, re-proposed at `v1.25.0`.** Unchanged, at H-9's line numbers. Two of its parts gain from
  the new releases:
  * the missing file's `-32002`, with `IsResourceNotFound` in its test;
  * H5's dead branches, which also clears 3 of H-2's uses.
* **D46 — Take the helpers that fix (H-5) or replace (H-6) Magic's own code (question 14).**
  * Auto-approval picks by kind alone: `allow_once`, then `allow_always`. When neither is offered
    it answers cancelled, rather than taking the first option. The proposal is to fail closed.
  * H-6's rows, each with a test of the behaviour it keeps or changes.
  * Not grouped select values, which 0208's P7 carries (H-5).
  * *Amended 2026-10-28, the SDK-gaps PLAN's C1b Deviation 2:* not `Process.ExitedFirst`, which joins D47.
* **D47 — H-7 is not decided here.** `clienthost`, elicitation's `Check`, the timelines,
  `ExtensionMux` and `acptest` each change behaviour Magic chose (H-7), and each takes its own
  MADR when wanted. This is D28's rule, applied to the new releases.
  * *Added 2026-10-28 (C1b Deviation 2):* `Process.ExitedFirst`. It cannot tell an agent's own death from a
    lost connection when the death closes the agent's output first, which is the common case.
* **D12 and gobble (question 15).** Proposed: Magic moves first, with this amendment's gates. gobble
  follows in its own repository, where H-3 meets its staticcheck. D12 does not hold until it does,
  as on 2026-10-21.

### Consequences of the amendment

* Good, because auto-approval can no longer select a reject option by its name (H-5).
* Good, because about a dozen hand-written protocol helpers give way to the SDK's, which its own
  consumer tests exercise.
* Good, because the pin is current, and a later bump starts from the deprecation boundary rather
  than meeting it.
* Neutral, because nothing on the wire changes except the decline names H-6 notes and D26's
  `-32002`.
* Bad, because D45 makes 0170 D5's rule one exception less strict. The exception's bound is the
  unstable-schema notice, which a planted test keeps honest.
* Bad, because the features D45 suppresses for, 0192's plans and D25's usage, rest on a schema the
  SDK calls compatibility-only. A later SDK that drops them would be a major version, which Magic
  would see at the bump.
* Bad, because D12 does not hold until gobble moves.

### Confirmation of the amendment

* `go list -m github.com/maccavelli/acp-go-sdk2` gives `v1.25.0`.
* `make staticcheck` passes. Every `//lint:ignore SA1019` and `//lint:file-ignore SA1019` names
  this amendment and a feature MADR. A planted use of a deprecated name outside the exception fails
  it.
* Fail-first tests, each seen failing on a scratch copy:
  * an option named "Don't allow" of kind `reject_once` is never auto-selected;
  * no allow kind answers cancelled;
  * plus D25's and D26's tests, as amendment B listed them.
* The SDK-gaps plan's gates; C3's live run on the owner's word. *Met: C0 to C2's gates, and C3's
  run of 2026-10-28.*

### Questions this amendment adds

These continue the numbering above.

* **13. The pin (D45).** `v1.25.0` with the narrow 0170 exception (proposed)? Or `v1.22.0`, with no
  exception? Or stay on `v1.20.0`?
* **14. Auto-approval with no allow option (D46).** Answer cancelled (proposed, fail closed)?
  Or keep today's first option?
* **15. gobble (D12).** Magic first, gobble after in its own repository (proposed)? Or both
  together, which waits on gobble?

*Answered 2026-10-28:*

* **13.** The owner first asked for the comparison: "change the sdk records and allow suppression,
  vs change the plan and do not suppress, without exceptions. pros vs cons. most flexible and
  extensible option. explain." Four no-suppression routes were set against D45's exception:
  * drop the features (0192's plans, the plan capability, D25);
  * freeze at `v1.22.0`;
  * decode around the names, which cannot set the deprecated `ClientCapabilities.Plan`;
  * give the names a non-deprecated route in the SDK first, which needs SDK accessors and a release
    against its own 0039.

  The owner chose "Path 1: narrow suppression (Recommended)". D45 stands as proposed. If the SDK
  ever adds a non-deprecated route, the suppressions are what is removed.
* **14.** "Answer cancelled (Recommended)". D46's auto-approval fails closed.
* **15.** "Magic first (Recommended)". D12 does not hold until gobble moves in its own repository.

The owner then chose "Stage records, then C0 (Recommended)": this amendment and the SDK-gaps
plan's revision are committed first, and C0 runs after.

### Evidence index of the amendment

* `acp-go-sdk2` `v1.21.0` to `v1.25.0`: the releases API's notes, saved as
  `leftovers/sdk_release_notes.md`; `v1.25.0` from the module proxy (`h1:xJ02C/fy…`, origin
  `8505722c`).
* The pin probe and its logs: `leftovers/sdk_pin_probe.py`, `leftovers/sdkprobe/`. The SA1019
  summary: `leftovers/sa1019_summary.py`.
* The capability inventory (H-6, H-7, H-8, H-9) came from a read-only subagent working in its own
  scratch clone. Its load-bearing claims were re-read here: `PromptResponse.Usage`'s notice; both
  H-5 faults; `PickPermissionOption`'s and `All`'s source; `Kill` at `acpagent.go:528`; 0184 D12.
  Its other line citations were not each re-read.
* gobble-cli's `go.mod`, from the contents API.

## Amendment — 2026-10-29 (I): `acp-go-sdk2 v1.26.0` and `go-selfupdate-lib v1.22.3`

*D48 and D49 decided by the owner, 2026-10-29; D50 ~~proposed~~ decided the same day (question 16,
below).* The owner asked, verbatim: "assess
the current lib/sdk dependency versions. they have been updated. evaluate how this changes our
current madr/plans and docs. pin newest versions." Its plan is
[0207-PLAN-pin-acp-go-sdk2-v1-26-0-and-go-selfupdate-lib-v1-22-3.md](0207-PLAN-pin-acp-go-sdk2-v1-26-0-and-go-selfupdate-lib-v1-22-3.md).

### Facts [observed 2026-10-29: `git ls-remote`, the GitHub releases, compare and contents APIs, scratch clones of both libraries, and a scratch copy of `d4f22efc`]

* **I-1. One new tag each.**

  | Library | Pinned | Newest | Peeled commit | Published |
  | :--- | :--- | :--- | :--- | :--- |
  | `acp-go-sdk2` | `v1.25.0` | `v1.26.0` | `59a43be7c0db69da245bfea798b975cd2fba08b2` | 2026-10-29, with release notes |
  | `go-selfupdate-lib` | `v1.22.2` | `v1.22.3` | `0620e96ba7eb47d45cc94b474dc2db87d029ce40` | tagged 2026-10-28; no GitHub release, a `CHANGELOG.md` entry |

  Magic pins them at `go.mod:11-12`, and the second also at `ci.yml:743` and `:764`. No other
  library of the owner's is a dependency: `go-llmprovider-sdk` is not in `go.mod`.
* **I-2. `acp-go-sdk2 v1.26.0` adds and changes nothing Magic uses.**
  * Its notes: the ACP schema is still `1.25.0`; `make api` reports eighteen compatible additions
    and no incompatible change; "Nothing sent or accepted on the wire changes".
  * `go doc -all` of every exported package at both tags (`leftovers/libs_assess.py`): no line
    removed, 28 added. The additions are `agentproc.InProcess`, `Command.StderrTail`,
    `StderrTailLines`, `JoinStderrTail`, `Process.StderrTail`, `StderrError`, `NewTranscript`,
    `ErrAbandoned`; `Connection.Shutdown`, `ServeAgent`, `ErrShutdown`; and in `acptest`,
    `ReadRecords`, `WriteRecords`, `Decode`, `Golden`, `ReplayAgent` and `ConnectReplay`.
  * `agentproc`'s `Process` now drives its agent through an unexported interface that a child and
    an in-process agent each implement. Its taps take any `io.ReadCloser` and `io.WriteCloser`
    (`agentproc/tap.go`). The notes say every existing `agentproc` test passes unchanged.
  * `go.mod` is unchanged between the tags.
* **I-3. `go-selfupdate-lib v1.22.3` changes code, not API.**
  * Its `CHANGELOG.md`: "The module's own tests only. No exported identifier and no behaviour a
    program can see changes: `make apicheck` reports `v1.22.3` compatible with `v1.22.2`."
  * `go doc -all`: no line removed or added.
  * But 30 non-test Go files change, 311 lines in and 252 out. `selfupdate`'s seams move from
    package variables onto the values that use them, and the `service` backends drop their test
    clocks. Of the packages Magic imports, `selfupdate`, `service`, `launchd`, `systemd` and
    `taskscheduler` are among them. So "no behaviour" is the library's claim, and Magic's tests are
    the check of it.
  * No file under `.github/workflows` and no `go.mod` line changes, so the reusable release
    workflows the two `ci.yml` pins name are the same.
* **I-4. What `go-selfupdate-lib` has decided for its next minor release.** Its
  `0038-MADR-pinned-plans-lifecycle-conformance-and-bubble-tests.md`, accepted 2026-10-27, puts
  only the bubbles and per-session seams in `v1.22.3`. The rest ships in `v1.23.0`:
  * root-relative replacement on Unix;
  * an exported lifecycle conformance check, `selfupdate/service/servicetest`;
  * a pinned update plan, `Plan` and `Apply`;
  * `Supervised.Start` returning `ErrNotInstalled` for a service its supervisor reports as not
    installed, where it runs `Up` today. Magic's runit, s6 and OpenRC backends run through
    `Supervised` (0212), so that bump will need a look at their `Start`.
* **I-5. The move, proven on a scratch copy of `d4f22efc`** (`leftovers/libs_pin_probe.py`; the
  copy was removed):
  * `go get` of both tags, then `go mod tidy`; `go mod tidy -diff` shows nothing.
  * The diff is `go.mod` 2 lines, `go.sum` 4, and `ci.yml` 2: the two pins and their comments.
  * `go build ./...`, and `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64: exit 0.
  * `make staticcheck`: exit 0, "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name",
    the count before the move. `v1.26.0` adds no deprecation Magic meets, and D45 is unchanged.
  * `./scripts/vulncheck.sh`: "clear", with the allowlisted `GO-2026-5932` as before.
  * `go test -race -count=1 ./...`: 44 packages ok, `TestWorkflowPinsMatchGoMod` among them.
* **I-6. What `v1.26.0` offers that Magic wrote itself, or lacks:**
  * **The agent's stderr, at its death.** Magic logs an agent's stderr at debug level and scans it
    for provider limits (`slogWriter`, `acpagent.go:496-501`, `:973-1014`). The exit message is
    "\<agent\> process exited: \<err\>", with no stderr (`:608-615`). `JoinStderrTail` would add the
    last lines to that error, and `Cmd.Stderr` would still get every byte.
  * **Both ways of the wire.** `wirecap` records only the agent's output (`OutputTap`,
    `acpagent.go:518`). `Command.InputTap` has existed since before `v1.25.0` and is unused. C3 of
    the SDK-gaps plan could not tell a `session/load` answer from a `session/resume` answer, because
    Magic's own requests were not in the capture (its Deviation 3). `NewTranscript` records both
    ways as `{"direction","frame"}` lines. Either way, the input side would need 0151's redaction
    and guard, and the committed fixtures' readers would need the new form.
  * **Replay.** `acptest.ReadRecords`, `Golden`, `ReplayAgent` and `ConnectReplay` replay a
    recorded agent against a client under test. They need two-way records, so they follow the item
    above. They would sit beside Magic's own `wireAgent` (`wireagent_test.go`), not replace it.
  * **A graceful close.** `Connection.Shutdown` answers the requests in flight before closing.
    Magic closes with `session/close` and then `Kill` (H-8).
  * **An agent in Magic's own process.** `InProcess` and `ServeAgent` are for running an agent
    written in Go as a goroutine. They would matter only if Magic ran gobble in its own process,
    which no record proposes. gobble's native integration is
    [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md)'s.
* **I-7. gobble.** gobble-cli is still at `6ee20140` (2026-10-23), pinning `acp-go-sdk2 v1.20.0` and
  `go-selfupdate-lib v1.17.0`. D12 holds no more than amendment H said.
* **I-8. The records and docs this touches:**
  * No page outside the records names either version: not `README.md`, `docs/architecture.md` or
    any guide. The code comments that name a version name where a behaviour began, and stay.
  * Every 0207 plan is `complete`, so this amendment takes a plan of its own, and `docs/README.md`'s
    index gains it.
  * [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
    is complete and unchanged. The captures its amendment of 2026-10-29 moved on (delete, resume,
    a turn ended by `session/cancel`) would gain from I-6's second item.
  * [0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md](0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md)
    is unchanged. A capture of both ways would be its subject.
  * [0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md)'s
    exception (D45) is unchanged, at 56 uses.
  * 0208 is unchanged; I-6's last item is noted there only if a record proposes it.

### The decisions this amendment makes

* **D48 — Pin `acp-go-sdk2 v1.26.0`.** `go.mod` and `go.sum` only; no source changes. D45's
  exception stands as it is. *Carried out 2026-10-29 by the plan's P1.*
* **D49 — Pin `go-selfupdate-lib v1.22.3`.** `go.mod`, `go.sum`, and both `ci.yml` pins, to
  `0620e96ba7eb47d45cc94b474dc2db87d029ce40` with the comment `# go-selfupdate-lib v1.22.3`
  (`TestWorkflowPinsMatchGoMod`). No source changes. I-3's production changes are checked by
  Magic's own tests on all three GOOS before it lands. *Carried out 2026-10-29 by the plan's P1,
  with the gates passing on macOS and on the Linux and Windows test hosts. Its Windows run
  first found two tests of the SDK-gaps plan failing there, on master too; they were fixed
  first, in `b0219278`, on origin `1566fd06` after the push's rebase (the plan's correction of 2026-10-29).*
* **D50 — I-6's items are not decided here (~~proposed~~ decided, question 16).** Each changes behaviour Magic
  chose, so each takes its own MADR when wanted, as D28 and D47 already rule. The proposed order:
  1. the stderr tail in an agent's exit message, the smallest and the one an operator sees;
  2. a capture of both ways, under 0151, so a fixture holds Magic's requests;
  3. replay tests from such captures.

  `Shutdown`, `InProcess` and `ServeAgent` wait for a record that needs them.

### Consequences of the amendment

* Good, because both pins are current, and the next bump starts from them.
* Good, because the move changes no source, and the probe shows every gate passing.
* Neutral, because nothing on the wire changes, and no exported API Magic uses changes.
* Bad, because `go-selfupdate-lib v1.22.3` rewrites the internals of the update and service paths
  under a "tests only" heading. Magic's own suites, and the Windows and Linux hosts, are what check
  that.
* Bad, because D12 still does not hold: gobble is six `acp-go-sdk2` minors behind.

### Confirmation of the amendment

* `go list -m github.com/maccavelli/acp-go-sdk2 github.com/maccavelli/go-selfupdate-lib` gives
  `v1.26.0` and `v1.22.3`.
* Both `ci.yml` pins name `0620e96b…`, and `TestWorkflowPinsMatchGoMod` passes.
* `make staticcheck` reports 56 ignored findings, as before.
* The plan's gates, on macOS, the Linux test host and the Windows test host.

### Questions this amendment adds

* **16. D50's order.** The stderr tail first, then a two-way capture under 0151, then replay
  (proposed)? Or a different order, or none yet?

*Answered 2026-10-29:*

* **16.** "Stderr, capture, replay (Recommended)". D50's order stands. None of the three starts
  until the owner asks for its record.
* **The plan:** "Approve, both hosts (Recommended)". P1 runs, with its gates on the Linux and
  Windows test hosts.

### Evidence index of the amendment

* The release notes of `acp-go-sdk2 v1.26.0`, from the releases API; `go-selfupdate-lib`'s
  `CHANGELOG.md` and 0038 MADR at `v1.22.3`, from a scratch clone.
* `leftovers/libs_assess.py` and its notes, `leftovers/libs/*-notes.md`: the logs, the non-test
  diffs and the `go doc` comparison of both libraries.
* `leftovers/libs_pin_probe.py` and its logs, `leftovers/libs_probe/`.
* gobble-cli's `go.mod`, from the contents API.

## Amendment — 2026-10-30 (J): `go-selfupdate-lib v1.23.1` and `acp-go-sdk2 v1.27.0`

*Decided by the owner, 2026-10-30, under
[0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)'s
D4 and D5.* The owner's words: "pin go-selfupdate-lib to current v1.23.1", and, when P3's `go get -u`
also moved `acp-go-sdk2`, "Take v1.27.0 (Recommended)". It is carried out by
[0217-PLAN-update-every-pin-to-its-current-release.md](0217-PLAN-update-every-pin-to-its-current-release.md)'s
P3, with its Deviations 6 and 7. I-4 predicted this bump.

### Facts [observed 2026-10-30: `git ls-remote`, the GitHub compare and contents APIs, the Go module proxy, the module cache, and the gates named in J-5]

* **J-1. One new minor each, both tagged after 0217's inventory.**

  | Library | Pinned | Newest | Tag object | Peeled commit |
  | :--- | :--- | :--- | :--- | :--- |
  | `go-selfupdate-lib` | `v1.22.3` | `v1.23.1` | `b0c08a0e…` | `c24f5919cd9e9e3ba3736e64bcf18a6e2a51489e` |
  | `acp-go-sdk2` | `v1.26.0` | `v1.27.0`, tagged 2026-10-30T17:01Z | `7c89dd4b…` | `913575f6c7fbf4c67ada357a3597293f43e09d2b` |

  Neither has a newer tag. `go-selfupdate-lib` has no GitHub release (the releases API answers
  404), only its `CHANGELOG.md` entry. `acp-go-sdk2`'s notes are its
  `docs/reports/0043-REPORT-release-notes-v1-27-0.md` at the tag.
* **J-2. `go-selfupdate-lib v1.23.0` changes behaviour Magic can see, and `v1.23.1` adds none.**
  0217's F3 lists `v1.23.0`'s changes from its `CHANGELOG.md`. Read in the module cache at
  `v1.23.1`, against `v1.22.3`:
  * `Supervised.Start` gains one check before `Up`: a `State` answered without error and with
    `Installed` false is `ErrNotInstalled` (`supervised_lifecycle.go:171-173`). With no supervisor
    to ask, `Up` still runs.
  * launchd's `Start` returns `ErrNotInstalled` before anything runs when the property list is
    missing (`launchd/lifecycle.go:279-283`). Its `Stop` bounds `bootout` and the wait with one
    deadline, not one each (`:179-186`).
  * systemd's `Start` of a unit that ran and failed before the start completed, as a `Type=notify`
    program that exits before it is ready, wraps `ErrUnhealthy` as well as the command's error
    (`systemd/lifecycle.go:184-198`). It was already an error.
  * `v1.23.1` changes only `go.mod`, `go.sum` and documents. It requires `golang.org/x/sys v0.49.0`,
    `x/term v0.47.0` and `x/mod v0.42.0`, and "`make apicheck` reports `v1.23.1` compatible with
    `v1.23.0`". No file under `.github/` changes in either release step, so the reusable release
    workflows are `v1.23.0`'s.
* **J-3. `acp-go-sdk2 v1.27.0` adds a package Magic does not import.** It adds `remote`, ACP over
  Streamable HTTP and WebSocket, and `example/remote`. Its notes: `make api` against `v1.26.0` is
  "0 incompatible, 11 compatible", each in `remote`; `agentproc/`, `acptest/`, `clienthost/`,
  `internal/` and the schema are unchanged. Its new requirement, `github.com/coder/websocket
  v1.8.15`, is one Magic already requires directly at that version. 0216's design, which reads
  `v1.26.0`'s `agentproc` and `acptest`, is unaffected.
* **J-4. Magic's callers of the changed behaviour, read at `cd2245ea`.**
  * The runit, s6 and OpenRC backends are `*libservice.Supervised` (`supervisedbackend.go:91-97`).
    Their `State` sets `Installed` from the definition file's existence (`supervisorstate.go:65-80`).
    Setup's `startThrough` (`setup.go:182-209`) starts only after setup wrote that definition, and
    the installer hook (`installerhook.go:148-197`) refreshes it before it starts. So `Start` is
    `ErrNotInstalled` only for a definition that is really missing, where `Up` ran and failed with
    a tool error before. Both callers wrap the error ("the service could not be started",
    "start the … service"), which stays true.
  * `stopForRemoval` (`setup.go:235-250`) already takes a `Stop` `ErrNotInstalled` as nothing to
    stop. The supervisor tool paths (`supervisortool.go:209`, `:227`) and the state readers
    (`supervisorstate.go:94`, `:206`) already map a missing definition to it.
  * launchd: the backend (`backend.go:94-109`) gives the property list setup writes. A missing one
    now fails at once rather than after `launchctl`'s retries. No caller of `Stop` relies on the
    old doubled bound: `setup.go:190` and `installerhook.go:166` fail on any error, and
    `setup.go:242` tolerates any but `ErrInsideService`.
  * systemd: no code in Magic branches on `ErrUnhealthy` from `Start`. `verifyStart`
    (`settle.go:116-135`) classifies only `WaitHealthy`'s errors.
  * The managed update calls `Start` inside the library (`selfupdate/managed.go:213`, `:351`), whose
    recovery handles any error. `internal/updateclient` checks no specific error.
  * Magic does not import `service/scm`, so its `Start` change does not reach Magic.

  No caller reports wrongly, so 0217's C8 stop did not arise.
* **J-5. The move, gated** (0217's P3 record holds each command and its output):
  * `go get -u ./...` and `go mod tidy`: `go 1.27.3` with no `toolchain` line, and no direct module
    behind. Both `ci.yml` pins name `c24f5919…` with `# go-selfupdate-lib v1.23.1`, and
    `git ls-remote` peels `v1.23.1^{}` to that commit. `TestWorkflowPinsMatchGoMod` passes.
  * This Mac: `make pre-add-check` (992 files), `go vet` for darwin/arm64, windows/amd64 and
    linux/amd64, `make staticcheck` (56 ignored findings, as I-5 counted), `make vulncheck`
    ("clear", with `GO-2026-5932`), `make race` (45 packages ok), `make preflight`, and
    `make live-supervisors KINDS="runit s6 openrc"` ("failed: none") all pass.
  * `make check-libs` on `HEAD` fails two checks for `go-selfupdate-lib`, as a move run before its
    commit does
    ([0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md),
    before its plan): the library moved alone fails `TestWorkflowPinsMatchGoMod`, and moves
    `x/mod`. `acp-go-sdk2 v1.27.0` passes every check there. Run on a scratch clone of `HEAD` with the phase's three files committed there, it
    reports "0 failing checks", each library "pinned at" its tag.
  * The Linux test host: `go test -race ./...` (45 ok), and `go test -count=1` of
    `internal/cli/service/...` and `internal/updateclient/...`. The Windows test host:
    `make ci-windows`, "ALL SELECTED CHECKS PASSED".
* **J-6. What the releases offer, and Magic does not take here.** `go-selfupdate-lib`'s pinned
  update plans (`Updater.Plan`, `Apply`, `ErrPlanStale`) and its `selfupdate/service/servicetest`
  conformance check; `acp-go-sdk2`'s `remote`. Each would change what Magic does, or how its
  backends are tested.

### The decisions this amendment makes

* **D51 — Pin `go-selfupdate-lib v1.23.1`.** `go.mod`, `go.sum`, and both `ci.yml` pins, to
  `c24f5919cd9e9e3ba3736e64bcf18a6e2a51489e` with the comment `# go-selfupdate-lib v1.23.1`. No
  source changes: J-4 finds no caller to fix.
* **D52 — Pin `acp-go-sdk2 v1.27.0`.** `go.mod` and `go.sum` only. D45's exception stands, at 56
  uses.
* **D53 — J-6's items are not adopted here.** Each takes its own record when wanted, as D28, D47
  and D50 already rule; 0217's D5 says the same for `Plan`, `Apply` and `servicetest`.

### Consequences of the amendment

* Good, because both pins are current again, and the next bump starts from them.
* Good, because a supervised or launchd start of a missing definition now says "not installed" at
  once, where it ran a tool or retried `launchctl` and then failed with a less direct error.
* Neutral, because `acp-go-sdk2 v1.27.0` adds only a package Magic does not import, and no
  requirement Magic did not already have.
* Bad, because `v1.23.0` changes how `Start` fails on every supervised backend and on launchd.
  J-4's reading and the live and host runs are what show Magic still reports it rightly.

### Confirmation of the amendment

* `go list -m github.com/maccavelli/acp-go-sdk2 github.com/maccavelli/go-selfupdate-lib` gives
  `v1.27.0` and `v1.23.1`.
* Both `ci.yml` pins name `c24f5919…`, and `TestWorkflowPinsMatchGoMod` passes.
* `make check-libs` exits 0 on the committed `HEAD`.
* After the push, CI's Go jobs and "Live supervisors (runit, s6)" pass (0217's P3 step 8).

### Evidence index of the amendment

* 0217's MADR, F3, F3a and F3b, and its PLAN's P3 record, with the logs `leftovers/0217-p3-*.log`.
* `leftovers/gsl_peel.py` and `leftovers/release_diff.py`: both libraries' tags peeled, the compares,
  `go-selfupdate-lib`'s `CHANGELOG.md` at `v1.23.1`, and `acp-go-sdk2`'s release notes at
  `v1.27.0`.
* `go-selfupdate-lib` at `v1.22.3` and `v1.23.1` in the module cache: `supervised_lifecycle.go`,
  `launchd/lifecycle.go`, `systemd/lifecycle.go` and `managed.go`.
* `leftovers/check_libs_staged.py`: go-shared's `check_libs.py` on a scratch clone of `HEAD` with
  the phase's files committed there.

## Amendment — 2026-10-30 (K): `acp-go-sdk2 v1.28.0`

*Decided by the owner, 2026-10-30,* under
[0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)'s
D4: "acp-go-sdk2 just got a new tagged release. we need to update the pin." It is carried out by
[0217-PLAN-update-every-pin-to-its-current-release.md](0217-PLAN-update-every-pin-to-its-current-release.md)'s
P3b (Deviation 14).

### Facts [observed 2026-10-30: `git ls-remote`, the GitHub compare and contents APIs, and this repository at `a7ca1f0d`]

* **K-1.** `v1.28.0`, annotated tag `d376db9a…`, peels to
  `b390bdf7d3046a7d28899190b7a15dbbbf18e8ff`, 11 commits past `v1.27.0`. Its notes are
  `docs/reports/0042-REPORT-release-notes-v1-28-0.md` at the tag.
* **K-2.** `make api` against `v1.27.0`: "0 incompatible, 3 compatible": `acp.ErrHandlersRunning`,
  `(*agentproc.Process).CloseContext` and `KillContext`. Nothing on the wire changes, and neither
  `go.mod` nor `go.sum` does.
* **K-3.** `Process.Wait`'s error stops repeating its cause for an in-process agent, and
  `ServeAgent`'s error gains `ErrHandlersRunning`. Magic runs its agents as children
  (`internal/provider/acpagent/acpagent.go:531`), tests the sentinels with `errors.Is` (`:644`),
  and calls neither `ServeAgent` nor `InProcess`, so neither change reaches it (0217 F3c).
* **K-4.** `CloseContext` and `KillContext` bound a close that a hung client handler would hold.
  Magic stops an agent with `Kill` (`internal/provider/acpagent/session.go:1267`, in a goroutine,
  and `acpagent.go:546`) and waits for it with `Wait` (`acpagent.go:634`); it calls no `Close`.

### The decisions this amendment makes

* **D54 — Pin `acp-go-sdk2 v1.28.0`.** `go.mod` and `go.sum` only. D45's exception stands.
* **D55 — K-4's API is not adopted here.** A bounded close changes how Magic ends a session, so it
  takes its own record when wanted, as D28, D47, D50 and D53 rule.

### Confirmation of the amendment

* `go list -m github.com/maccavelli/acp-go-sdk2` gives `v1.28.0`; `make staticcheck` reports 56
  ignored findings; `make race` and both test hosts pass; `make check-libs` exits 0 on the
  committed `HEAD`.
