---
status: in-progress
date: 2026-09-19
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0159 — The Windows service path is half-wired: it installs a daemon it cannot refresh, cannot log, and mis-describes

Implements [0159-MADR-the-windows-service-path-is-half-wired.md](0159-MADR-the-windows-service-path-is-half-wired.md)
decisions D1, D2 and D4–D14, closing findings F1, F2, F4, F5, F7, F9–F17 and
F19, and bounding F18. D3 (F3, F8) is delivered by MADR/PLAN 0157 and is not
re-planned here. F6 is a closed hypothesis.

Every behavioural claim this plan relies on was measured on this host on
2026-09-19 (MADR "Re-measured 2026-09-19", probes 4–12). The three that could
not be are marked **[unverified]** in the MADR, and are verified by the phase
that depends on them. No step assumes one.

## Goal

The finish line, as observable states on this Windows host (Windows 11 Home
26200, `MAC420\macsm`, unprivileged). "Release 1" is the build after P11;
"release 2" is the build after P13.

1. `mcremote setup-service --refresh --json` exits 0 with a verdict, not "only
   supported on Linux and macOS" (release 1).
2. Running `mcremote setup-service` twice in a row, with no flags and no
   `--force`, reports the task unchanged on the second run and writes nothing
   (release 1).
3. `pwsh -File scripts\acceptance-windows-service.ps1` passes. It drives
   mcrelay through register → `Running` → refresh `unchanged` → daemon killed
   and relaunched within 120 s → `--remove`, with no elevation and no task left
   behind (release 1).
4. `scripts\acceptance-windows.ps1` parses under PowerShell 5.1 and 7. Every
   check except `paths --json (C1)`'s `log_dir` passes; that one belongs to
   MADR 0157 P1.
5. `go test -v ./internal/admin/ ./internal/fsutil/` shows
   `TestSocketIdentityStable` and `TestWriteFileAtomic` as PASS, not SKIP.
6. The Windows `setup-service` summary contains no `systemctl`, `journalctl`,
   `loginctl`, `.service` or `make install`.
7. `--unit-name` other than the product is an error on Windows. `--env` works
   on Windows (release 2).
8. The rendered principal is the token's SID whatever `USERNAME` says.
9. No document claims an `MC_WINDOWS_SIGN_*` hook exists.
10. The task-launched daemon creates no console host (release 2, after MADR
    0157 P4).
11. `make ci-windows` exits 0 after every phase, and the WSL Linux suite passes
    after every Go phase.

## Scope

### In scope (the only files any phase may touch)

Each phase touches only the files listed for it. "New" means created by that
phase.

* **P1:** `scripts/acceptance-windows.ps1` (line 65 only).
* **P2:**
  * `docs/ops-windows-install.md` (the signing paragraph, `:40-43`);
  * `docs/spec/0116-MADR-windows-and-linux-arm64-build-targets.md` (an
    appended amendment section only).
* **P3:** `internal/admin/owner_windows.go`, `internal/admin/owner_windows_test.go`,
  `internal/admin/admin.go` (the two `socketIdentity` call sites only).
* **P4:** `internal/fsutil/atomic_acl_windows_test.go` (new).
* **P5:**
  * `internal/cli/service/setup_schtasks.go`, `internal/cli/service/schtasks.go`;
  * `internal/cli/service/task_compare_test.go` (new);
  * `internal/cli/service/testdata/task-export-v0174.xml` (new).
* **P6:**
  * `internal/cli/service/schtasks.go`;
  * `internal/cli/service/principal_windows.go` (new),
    `internal/cli/service/principal_other.go` (new);
  * `internal/cli/service/schtasks_test.go`.
* **P7:**
  * `internal/cli/service/result_print.go`,
    `internal/cli/service/result_print_test.go` (new),
    `internal/cli/service/testdata/result_print_systemd.golden` and
    `result_print_launchd.golden` (new);
  * `internal/cli/service/setup.go` (`normalize` only);
  * `internal/cli/service/setup_test.go`;
  * `docs/ops-windows-install.md` (the "Running in the background" section).
* **P8:** `internal/cli/service/control_schtasks.go`,
  `internal/cli/service/schtasks_test.go`,
  `internal/cli/service/control_schtasks_windows_test.go` (new).
* **P9:**
  * `internal/cli/service/schtasks.go`,
    `internal/cli/service/control_schtasks.go`;
  * `internal/cli/service/setup_schtasks.go` (the comparison fields only);
  * `internal/cli/service/schtasks_test.go`,
    `internal/cli/service/task_compare_test.go`;
  * `docs/ops-windows-install.md` (the restart and stop wording).
* **P10:**
  * `internal/cli/service/refresh.go`,
    `internal/cli/service/refresh_schtasks.go` (new),
    `internal/cli/service/refresh_schtasks_test.go` (new);
  * `internal/cli/service/testdata/task-export-v0174.xml`;
  * `docs/ops-windows-install.md` (a new "Updating" section).
* **P11:** `scripts/acceptance-windows-service.ps1` (new).
* **P12:**
  * `internal/cli/service/setup.go`, `internal/cli/service/schtasks.go`,
    `internal/cli/service/setup_schtasks.go`,
    `internal/cli/service/refresh_schtasks.go`;
  * the `*_test.go` beside each;
  * `internal/cli/serve.go`, `internal/relay/cli.go` (the serve command's
    flags only);
  * `internal/cli/envfile.go` (new) and its `_test.go`;
  * `docs/ops-windows-install.md`.
* **P13:**
  * `internal/cli/serve.go`, `internal/relay/cli.go` (the serve command's
    flags only);
  * `internal/cli/detach_windows.go` (new), `internal/cli/detach_other.go`
    (new);
  * `internal/cli/service/schtasks.go` and its test;
  * `scripts/acceptance-windows-service.ps1`;
  * `docs/ops-windows-install.md`.

Every phase may also append to this file's `## Execution record`, and set this
pair's `status` and `date`.

### Out of scope

* **The Windows log sink, `LogDir` unification, and the `log_dir` acceptance
  check.** These are MADR/PLAN 0157 (D3). P13 waits for 0157 P4.
* **The 0157 text that says the Windows task "has no console".** It is
  corrected in 0157's own next revision (MADR 0159, Related records).
* **SCM or Windows Service support, boot-start, and `windows/arm64`.** MADR
  0116 decisions, unchanged.
* **Honouring `--unit-name` on any platform's lifecycle.** D13 refuses it on
  Windows. Making the lifecycle follow custom names is a separate record.
* **A Windows `servicePathExtras`.** It is not needed on the measured evidence
  (MADR probe 9).
* **The three load-sensitive test timeouts** seen once in probe 12. They are
  not a 0159 finding (see Deferred).
* **The macOS and Linux service paths**, except that C5 proves they are
  unchanged.
* **Authenticode signing itself.** Deferred to a certificate (D10).

## Stability rule

Baseline, measured 2026-09-19 at `38078c5` on this host: `make ci-windows`
reports "ALL SELECTED CHECKS PASSED" (MADR 0162), and `go test ./...` gives 41
`ok` with no failures. The WSL `Ubuntu-24.04` clone gives 41 `ok`. After every
phase: **no failure at all** on either, with one exception, stated below.

Every Go phase (P3–P10, P12, P13) ends with, from the repository root in Git
Bash:

```bash
git diff --cached --name-only --diff-filter=AM -z -- '*.go' | xargs -0 -r gofmt -l   # → nothing
go build ./... && go vet ./...                                                          # → exit 0
for os in linux darwin; do GOOS=$os CGO_ENABLED=0 go vet ./internal/cli/... ./internal/admin/ ./internal/fsutil/ || echo "VET $os FAILED"; done   # → nothing
go test ./... 2>&1 | grep -E '^(--- FAIL|FAIL)'                                         # → nothing
go test -race ./internal/cli/... ./internal/admin/ ./internal/fsutil/ ./internal/updateclient/ 2>&1 | grep -E '^(--- FAIL|FAIL)'   # → nothing
make pre-add-check FILES="$(git diff --cached --name-only --diff-filter=AM -- '*.go' | tr '\n' ' ')"
make ci-windows                                                                         # → exit 0, "ALL SELECTED CHECKS PASSED"
```

Then the WSL lane (memory `wsl-linux-test-lane`): apply the staged diff to a
fresh ext4 clone and run `go test ./...`, which must give 41 `ok` (or more
where a phase adds a package) with no failure.

**The one exception.** `internal/provider/codex`
`TestProviderInitializesBeforeTimingOut`, `internal/relay`
`TestHandleTunnelRejections` and `internal/relayhost`
`TestBridgeFrameLimitFollowsConfig` failed once, under load, in probe 12.

* If any of them fails, rerun it alone three times.
* 3/3 passing alone is recorded in the Execution record and is **not** a
  failure of the phase.
* A failure alone **is** a failure of the phase.

Docs-only phases (P1, P2) end with `git diff --stat` touching only their
files, plus their own checks.

**Commits:** one commit per phase. Stage exactly that phase's paths, then run
`git commit --no-edit`; the global `prepare-commit-msg` hook writes the
message. Before the first commit, confirm that
`git rev-parse --path-format=absolute --git-path hooks` resolves to
`~/.global-git-hooks`. Never use `-m` or `--amend`. The pair is committed alone
before P1 (bootstrap exception). **`git push`, tags and releases need an
explicit owner instruction.** The release boundaries in Rollout are owner
actions.

## Cross-cutting contracts

* **C1 — Every commit is green.** The Stability rule holds after every phase,
  on Windows and in WSL.
* **C2 — No elevation, ever.** `RunLevel LeastPrivilege` and
  `LogonType InteractiveToken` stay. No phase adds a command that needs an
  administrator. The P11 script refuses to run elevated. Its first check fails
  if `[Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole('Administrators')`
  is true in an elevated token.
* **C3 — The operator's live `mcremote` task is not modified by any automated
  step.** The only interactions any phase may have with it are:
  * read-only queries (`Get-ScheduledTask`, `schtasks /query`);
  * `--print-only`;
  * `setup-service` **without** `--force`. That either reports unchanged and
    writes nothing, or refuses before writing (`setup_schtasks.go:23-28`).
    Prove it by exporting the definition before and after and comparing with
    `cmp`.

  Any write to the live task is the manual, owner-approved update step in
  Rollout.
* **C4 — The task's `serve` arguments do not change before release 2 (F18).**
  After each of P1–P11, the rendered `<Arguments>` for the default options are
  byte-identical to v0.17.4's (`serve --config <path>`). Check by comparing
  the `<Arguments>` line of `setup-service --print-only` from the phase's
  build against the installed v0.17.4's.
* **C5 — Unix service renderings do not change.** The systemd unit and launchd
  plist output of `RenderUnit` for the existing test fixtures are unchanged:
  the existing tests in `setup_test.go` and `refresh_test.go` pass unmodified.
  No phase edits a systemd or launchd template.
* **C6 — No test is weakened.**
  * A skip is replaced by an assertion, never by a deleted test.
  * The only tests a phase may delete are tests of code it deletes. P8 deletes
    `taskStatusRunning`, and therefore `TestTaskStatusRunning`.
  * If a new assertion fails (P3, P4, and the P9/P13 live checks), the phase
    stops and records a finding. It does not loosen the assertion.
* **C7 — Probe tasks are temporary.** Any throwaway scheduled task a phase
  creates is named `mcr-probe-*`, and is deleted with its processes before the
  phase commits. `(Get-ScheduledTask -TaskName 'mcr-probe-*' -ErrorAction
  SilentlyContinue).Count` must be 0.

**C3 is the contract most at risk.** When a phase's code is done, the fastest
"real" check is `mcremote setup-service --force` on the live task. That
replaces the operator's running daemon definition, possibly with a
half-finished rendering. The permitted live checks are exactly the three
listed. **C4 is second.** Adding `--env-file` or `--detach-console` to the
arguments "while we're in there" re-opens F18 for every host that updates
straight from v0.17.x.

## Dependency and delivery order

```text
P1 ─ P2 ─ P3 ─ P4                                       (independent; any order)
P5 ──► P6 ──► P7 ──► P8 ──► P9 ──► P10 ──► P11 ──► [release 1]
                                                       │
MADR/PLAN 0157 P4 (file sink) ─────────────────────────┤
                                                       ▼
                                         P12 ──► P13 ──► [release 2]
```

* P5 comes first among the service phases: P9's new trigger and P10's
  `unchanged` verdict are only meaningful once the comparison is semantic
  (F19).
* P6 comes before P7, because P7's summary shows the principal.
* P8 comes before P9, because P9's stop and start and P11's gate use the new
  status probe.
* P10 comes after P9, so the refresh reproduces the final release-1 task shape
  and recovers older shapes, including the v0.17.4 export.
* P11 comes last in release 1, because it exercises P5–P10 together.
* P12 and P13 come after release 1 (C4, F18).
* P13 also requires 0157 P4, because after `FreeConsole` stderr goes nowhere.

## Implementation Steps

### P1 — The acceptance script parses (D11 part 1; closes F16)

1. In `scripts/acceptance-windows.ps1`, change line 65 from
   `throw "$Label: got '$g' want '$w'"` to `throw "${Label}: got '$g' want '$w'"`.
   Change nothing else.

**Verification (P1):**

```powershell
foreach ($ps in 'powershell','pwsh') { & $ps -NoProfile -Command "`$e=`$null; [void][System.Management.Automation.Language.Parser]::ParseFile('scripts\acceptance-windows.ps1',[ref]`$null,[ref]`$e); `$e.Count" }   # → 0, 0
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts\acceptance-windows.ps1
#   → every check PASS except "mcremote paths --json (C1)" with the log_dir mismatch
#     (owned by MADR 0157 P1). A "go test ./..." failure falls under the Stability rule's exception.
```

Record the script's actual output in the Execution record. The expected
failure is **not** fixed here.

### P2 — The signing documentation stops claiming a hook (D10; closes F13)

1. In `docs/ops-windows-install.md:40-43`, replace "Signing is designed into the
   build (`MC_WINDOWS_SIGN_*`)" with a statement that the binaries are not yet
   Authenticode-signed, and that a signing hook waits for a code-signing
   certificate (MADR 0159 D10). Keep the rest of the paragraph.
2. Append to `docs/spec/0116-MADR-windows-and-linux-arm64-build-targets.md`
   exactly one section: `## Amendment — 2026-09-19: the D14 signing hook waits
   for a certificate`. It should cite MADR 0159 F13 and D10, and state that no
   `MC_WINDOWS_SIGN_*` hook exists, because a hook nothing can call cannot be
   verified. Change no other line of 0116 (additive rule).

**Verification (P2):**

```bash
git grep -n 'MC_WINDOWS_SIGN' -- docs/ops-windows-install.md     # → only the "not yet" wording
git diff --stat                                                  # → the two files
git diff -U0 -- docs/spec/0116-MADR-*.md | grep -c '^-[^-]'      # → 0 (no removed lines)
```

### P3 — `socketIdentity` reads a real file index (D4; closes F4, F5)

1. In `internal/admin/owner_windows.go`, reimplement `socketIdentity(fi)`. It
   takes the path the caller already has; where only `fi` is available, add a
   `socketIdentityPath(path string) (uint64, bool)` and change the two callers
   in `admin.go` (`:129-134`, `:146-150`) to pass the socket path. Steps:
   `windows.CreateFile(path, 0, FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,
   nil, OPEN_EXISTING, FILE_FLAG_OPEN_REPARSE_POINT|FILE_FLAG_BACKUP_SEMANTICS, 0)`,
   then `windows.GetFileInformationByHandle`, then the identity
   `uint64(FileIndexHigh)<<32 | uint64(FileIndexLow)`, then close the handle.
   Any failure returns `(0, false)`.
2. In `owner_windows_test.go`, replace the `t.Skip` at `:57-59` with
   `t.Fatalf`. Add `TestSocketIdentityOfALiveSocket`:
   * listen on a temp AF_UNIX socket;
   * expect a non-zero identity that is equal across two calls;
   * expect a second socket's identity to differ.
3. If the flags in step 1 do not yield `ok` on this host, stop. Record the
   exact error and amend the MADR before changing the approach (C6).

**Verification (P3):** the Stability rule, plus:

```bash
go test ./internal/admin/ -run 'TestSocketIdentity' -v -count=1 | grep -E '^--- '   # → PASS ×2, no SKIP
```

### P4 — The Windows ACL contract of atomic writes is asserted (D12; closes F15)

1. Create `internal/fsutil/atomic_acl_windows_test.go`
   (`//go:build windows`, package `fsutil_test`, which is external so it can
   import `appdirs` without a cycle). It adds `TestWriteFileAtomicIsOwnerOnlyInPrivateDir`:
   * `dir := t.TempDir()` + `appdirs.EnsurePrivateDir(filepath.Join(dir,"p"))`;
   * `fsutil.WriteFileAtomic(path, data, fsutil.AtomicOptions{Perm: 0o600})` a
     file there;
   * assert `appdirs.FileIsOwnerOnly(path)` is true;
   * repeat for an overwrite of an existing file.
2. Leave `atomic_test.go`'s POSIX-mode tests as they are. They assert mode
   bits, which do not exist on Windows, so their skips are correct. The new
   test is the Windows half of the contract.
3. If the assertion fails, stop. It is a finding about `WriteFileAtomic` or
   `EnsurePrivateDir` on Windows, and gets its own record (C6).

**Verification (P4):** the Stability rule, plus
`go test ./internal/fsutil/ -run OwnerOnly -v -count=1` → PASS.

### P5 — Task definitions are compared semantically (D14; closes F19)

1. Add `taskFieldsFromXML(s string) (taskFields, error)` to
   `setup_schtasks.go`. It parses with `encoding/xml` into a struct holding
   exactly the fields `renderTaskXML` controls:
   * the `LogonTrigger` and its `UserId`;
   * any `TimeTrigger`, with `StartBoundary` and `Repetition/Interval`;
   * the principal `UserId` and `LogonType`, and `RunLevel`, defaulting to
     `LeastPrivilege` when absent;
   * `MultipleInstancesPolicy`, `DisallowStartIfOnBatteries`,
     `StopIfGoingOnBatteries` and `AllowHardTerminate` (default `true`);
   * `StartWhenAvailable`, `RunOnlyIfNetworkAvailable` (default `false`),
     `Enabled` (default `true`) and `Hidden` (default `false`);
   * `ExecutionTimeLimit` and `RestartOnFailure`;
   * `Exec` `Command`, `Arguments` and `WorkingDirectory`.

   Elements that Task Scheduler adds (`URI`, `IdleSettings`,
   `UseUnifiedSchedulingEngine`, `Author`, `Description`) are ignored.
2. `sameTaskDefinition(existing, want)` compares `taskFields`. Principal
   `UserId`s compare equal when both resolve to the same SID. That resolution
   goes through a seam (`sidForAccount func(string) (string, error)`), which
   is identity for an input that is already a SID, and backed on Windows by
   `windows.LookupSID`. Keep `normalizeTaskXML` only if something else still
   calls it (check with `git grep`); otherwise delete it with its tests (C6).
3. Save this host's export as `testdata/task-export-v0174.xml`: the live
   `schtasks /query /tn mcremote /xml ONE` from MADR probe 5. Replace the SID
   with `S-1-5-21-1-2-3-1001`, the account with `HOST\user`, and the paths
   with `C:\Users\user\…`, and keep everything else byte for byte.
4. `task_compare_test.go`:
   * The default options rendered, against the fixture, are **equal**. The
     seam maps `HOST\user` to `S-1-5-21-1-2-3-1001`.
   * Changing `Arguments`, `Command`, `WorkingDirectory`, `RestartOnFailure`
     or a trigger makes them **differ**.
   * A definition with a foreign principal differs.

**Verification (P5):** the Stability rule, plus these, with C3's
before-and-after export `cmp`:

```bash
go test ./internal/cli/service/ -run 'TaskDefinition|TaskFields' -v -count=1 | grep -E '^--- '
B=$(mktemp -d); go build -o $B/mcremote.exe ./cmd/mcremote
schtasks //query //tn mcremote //xml ONE > $B/before.xml
$B/mcremote.exe setup-service --binary "$LOCALAPPDATA/Programs/mcremote/mcremote.exe"; echo "exit=$?"   # → 0; summary says unchanged
schtasks //query //tn mcremote //xml ONE > $B/after.xml; cmp $B/before.xml $B/after.xml && echo UNCHANGED   # → UNCHANGED
```

`--binary` points at the installed path, so the render names the same command
as the live task. Without `--force`, a mismatch refuses before writing, so C3
holds either way.

### P6 — The principal comes from the process token (D8; closes F11)

1. Add `principal_windows.go` (`//go:build windows`), defining
   `currentTaskPrincipal() (sid, account string, err error)`:
   `appdirs.CurrentUserSID()`, then `(*windows.SID).LookupAccount("")`, giving
   `domain + "\\" + name`. Add `principal_other.go`, a stub returning an error;
   it is only reached through `installOS` overrides in tests, and those tests
   inject the seam.
2. In `schtasks.go`, replace `currentTaskUser()` with a package variable
   `taskPrincipal = currentTaskPrincipal`. `renderTaskXML` takes `(sid,
   account)`:
   * `Principal.UserId = sid`;
   * `LogonTrigger.UserId = account`;
   * `RegistrationInfo.Author = account`.

   A lookup error fails setup, refresh and `--print-only` with a clear message
   before any `schtasks` call. `USERNAME` and `USERDOMAIN` are no longer read.
3. Tests in `schtasks_test.go`:
   * with the seam injected, the rendered principal is the SID;
   * with `t.Setenv("USERNAME","bogus")` the render is unchanged;
   * a seam error propagates and `runSchtasks` is never called;
   * P5's fixture comparison still reports equal.

**Verification (P6):** the Stability rule, plus:

```bash
USERNAME=bogus USERDOMAIN= $B/mcremote.exe setup-service --print-only | grep -E '<UserId>'
#   → Principal UserId = this host's SID (S-1-5-21-1365755026-…-1001); LogonTrigger UserId = MAC420\macsm
```

Then P5's live idempotency check again, which must stay UNCHANGED.

### P7 — Windows summary, and flags that no longer vanish (D2, D13; closes F2, F17; stops F9's silent drop)

0. **Before editing**, capture the current output of `PrintSetupResult` for
   the default (systemd) and `launchd-agent` scopes as
   `internal/cli/service/testdata/result_print_systemd.golden` and
   `..._launchd.golden`, with a throwaway test run under an `-update` flag, and
   commit them in this phase (C5). Both golden files join P7's scope.
1. In `result_print.go`, add a `case "windows-task":` arm to every scope switch
   (`:48-57` and `:75-135`). It prints:
   * `Task:` `Task Scheduler\<name>`;
   * `Scope:` `per-user Task Scheduler task (at logon, no elevation)`;
   * `Status:` `schtasks /query /tn <name>`;
   * `Start:` `schtasks /run /tn <name>`;
   * `Stop:` `schtasks /change /tn <name> /disable` then `schtasks /end /tn <name>`;
   * `Remove:` `<product> setup-service --remove`;
   * `Logs:` "not yet written to a file on Windows (MADR 0157)";
   * `Install/update the binary with install.ps1`.

   The `Stop:` line is written for P9's semantics. Until P9, `/disable` is
   harmless, because the task has no repeating trigger yet.
2. In `setup.go` `normalize`, when the install OS is `windows`:
   * a `UnitName` that is not empty and not equal to `Product` is an error:
     "--unit-name is not supported on Windows: the task is always named
     <product> (MADR 0159 D13)";
   * a non-empty `ExtraEnviron` is an error: "--env is not yet supported on
     Windows (MADR 0159 D6)". P12 removes this second rule.
3. `result_print_test.go`:
   * the `windows-task` output contains each line above;
   * it contains none of `systemctl`, `journalctl`, `loginctl`, `.service` or
     `make install`;
   * the default (systemd) and `launchd-agent` outputs still match step 0's
     golden files (C5).
4. `setup_test.go`: under `OverrideInstallOS("windows")`, cover the two
   refusals; `--unit-name mcremote` is accepted.
5. `docs/ops-windows-install.md`, "Running in the background": replace any
   systemd-shaped instruction with the step 1 commands, and say that `--env`
   and `--unit-name` are refused on Windows and why.

**Verification (P7):** the Stability rule, plus:

```bash
$B/mcremote.exe setup-service --print-only --unit-name other; echo "exit=$?"   # → non-zero, the D13 message
$B/mcremote.exe setup-service --print-only --env A=b; echo "exit=$?"            # → non-zero, the D6 message
go test ./internal/cli/service/ -run 'PrintSetupResult|Normalize' -v -count=1 | grep -E '^--- '
```

### P8 — Task state is probed without parsing localised text (D9; closes F12)

1. In `control_schtasks.go`, add a seam, `taskState = func(name string) (state
   int, found bool, err error)`. It runs `powershell.exe -NoProfile
   -NonInteractive -Command "$t = Get-ScheduledTask -TaskName '<name>'
   -ErrorAction SilentlyContinue; if ($t) { [int]$t.State } else { 'absent' }"`,
   with `<name>` validated against the product-name pattern first.
   * Output `absent` gives `found=false`.
   * An integer gives `found=true`.
   * Anything else, or a non-zero exit, gives `err`.
2. `isInstalledWindows` returns `found`. `isActiveWindows` returns
   `found && state == 4`. **Both return `err` instead of swallowing it.** Delete
   `taskStatusRunning` and `TestTaskStatusRunning` (C6).
3. `schtasks_test.go`: inject `taskState`, and cover running (4), ready (3),
   absent, and an error that propagates.
4. `control_schtasks_windows_test.go` (`//go:build windows`): call the real
   seam for the task name `mcr-no-such-task-<random>`, and expect `found=false`
   and `err=nil`.

**Verification (P8):** the Stability rule, plus:

```bash
go test ./internal/cli/service/ -run 'TaskState|IsActive|IsInstalled' -v -count=1 | grep -E '^--- '
go test ./internal/updateclient/ -count=1                  # → ok (WaitHealthy unchanged; uses IsActive)
```

### P9 — A crashed daemon comes back, and stop means disable (D5; closes F7)

1. `schtasks.go`:
   * `taskDefinition.Triggers` gains a `TimeTrigger` with a constant
     `StartBoundary` `2000-01-01T00:00:00` (constant, so the render stays
     idempotent), `Enabled true`, and `Repetition` `Interval PT1M`,
     `StopAtDurationEnd false`, with no `Duration`.
   * `MultipleInstancesPolicy` stays `IgnoreNew`.
   * Replace the `RestartOnFailure` comment with the measured truth: it covers
     launch failures, not exits (MADR probe 8), and the repeating trigger is
     what restarts the daemon.
2. `control_schtasks.go`:
   * `stopWindows` runs `schtasks /change /tn <name> /disable`, then `/end`. A
     task that is not running is not an error.
   * `startWindows` runs `/change /tn <name> /enable`, then `/run`.
   * Update the TerminateProcess comment to say stop now also disables.
3. `setup_schtasks.go`: the D14 comparison includes the `TimeTrigger` fields.
   Setup already runs `startWindows`, so it re-enables.
4. Tests: the rendered XML has both triggers, and the constant boundary is
   stable across renders. Stop issues `/disable` then `/end` and start issues
   `/enable` then `/run`, in that order, both checked through `runSchtasks`.
   P5's fixture, which has no `TimeTrigger`, now compares **different** from
   the render; P10 depends on that.
5. **Live check of the unverified piece** (MADR open question 5, C7). This
   checks a fixed *past* `StartBoundary`:
   1. Render with `--print-only`. Replace the `Command` with the MADR probe
      program (`probe.exe <log> p9 0 1`), and register it as
      `mcr-probe-p9-watchdog`.
   2. Run it once, and wait 150 s.
   3. Expect at least 2 runs in the log (a relaunch after exit). Then disable
      it, wait 90 s, and expect no new run. Delete it.

   If it does not relaunch, stop: the boundary form is wrong. Amend the MADR
   with the measurement before choosing another.
6. `docs/ops-windows-install.md`: restarts happen within about a minute; stop
   with the P7 commands; a bare `schtasks /end` is undone within a minute.

**Verification (P9):** the Stability rule, step 5's recorded result, and C4:
the rendered `<Arguments>` line is unchanged from v0.17.4.

### P10 — `--refresh` works on Windows, and a refreshed task can be restored (D1; closes F1; bounds F18)

1. `refresh.go`:
   * `RefreshUnit` adds `case "windows": return refreshSchtasks(opts, ro)`.
   * `RestoreUnitBackup(path, backup)`: when `path` has the prefix
     `Task Scheduler\`:
     1. read `backup` (text);
     2. write it through `encodeTaskXML` to a temp file;
     3. run `schtasks /create /tn <name from path> /xml <tmp> /f`;
     4. remove `backup`;
     5. return an error that names both files on failure.
   * Every other path is unchanged.
2. `refresh_schtasks.go`, `refreshSchtasks(opts, ro)`:
   1. Run `schtasks /query /tn <product> /xml ONE`. Not registered gives
      `VerdictNone`, with `Path` `Task Scheduler\<product>`.
   2. The definition is managed only if its `Description` is
      `"<product> background service (magic-cli-remote)"`, it has exactly one
      `Exec`, and its `Arguments` start with `serve`. Otherwise the verdict is
      `VerdictKept`, with the reason.
   3. Recover `Options` from the `Exec`:
      * `Binary` = `Command`, and `WorkingDirectory`;
      * parse `Arguments` with exactly the flags `serveArgs` writes
        (`--config`, `--data-dir`, `--listen-host`, `--listen-port`,
        `--log-level`, `--log-format`), honouring the double-quoting of
        `quoteArg`;
      * any other token gives `VerdictKept` ("carries arguments this binary
        did not write").
   4. Render with `renderTaskXML`, and compare with the D14 comparison. Equal
      gives `VerdictUnchanged`.
   5. Otherwise the verdict is `VerdictRefreshed`, `Changed`. With
      `PrintOnly`, return here with `Body` set.
   6. Otherwise:
      1. write the exported text as the backup, at
         `<StateDir>\<product>-task.prev.xml`, where `StateDir` comes from
         `appdirs` for the product (`Paths.StateDir`), the directory is created
         with `EnsurePrivateDir`, and the file is written with
         `fsutil.WriteFileAtomic(…, fsutil.AtomicOptions{Perm: 0o600})`;
      2. register the new body through `encodeTaskXML` + `/create /f`;
      3. set `BackupPath`.
3. `refresh_schtasks_test.go`, driven through the `runSchtasks` and
   `taskPrincipal` seams:
   * absent gives none;
   * a foreign `Description` gives kept;
   * P5's v0.17.4 fixture gives **refreshed**, the new body carries both
     triggers, and the recovered `Arguments` equal the fixture's;
   * the refreshed body fed back in gives **unchanged**;
   * an unknown argument gives kept;
   * `PrintOnly` issues no `/create`;
   * `RestoreUnitBackup` with a `Task Scheduler\` path issues `/create` with
     the backup's content and removes the backup.
4. `docs/ops-windows-install.md` gains an **Updating** section:
   * `mcremote update` stops (disables and ends), swaps, refreshes the task,
     starts, and waits for `Running`;
   * on failure it rolls back;
   * the F18 caveat and its recovery (`setup-service --force` with the binary
     left in place), for a host updating from v0.17.x.

**Verification (P10):** the Stability rule, plus these live read-only and
print-only checks (C3):

```bash
$B/mcremote.exe setup-service --refresh --print-only --json
#   → {"verdict":"refreshed","changed":true,...}: the live task lacks P9's trigger; nothing is written
schtasks //query //tn mcremote //xml ONE | cmp - $B/before.xml && echo "live task untouched"
```

C4: the refreshed body's `<Arguments>` equals the live task's.

### P11 — A gate for the service lifecycle, through mcrelay (D11 part 2; closes F14)

1. Create `scripts/acceptance-windows-service.ps1`. It is standalone and
   modelled on `acceptance-windows.ps1`'s `Invoke-Check` and counting. In
   order:
   * **S0:** refuse to run elevated (C2); refuse if a `mcrelay` task is
     already registered (it is not the script's to touch); refuse if
     `mcremote`'s definition differs at the end (C3). Export it at the start,
     and `cmp` at the end.
   * **S1:**
     * build `mcrelay.exe` into `$T = New-Item -ItemType Directory (Join-Path
       $env:TEMP "mcr-accept-<guid>")`;
     * make `$T\cfg` private: `icacls $T\cfg /inheritance:r /grant:r
       "*<own SID>:(OI)(CI)F"`, with the SID from
       `[Security.Principal.WindowsIdentity]::GetCurrent().User.Value`;
     * write `$T\cfg\config.yaml` with the relay test fixture's shape
       (`internal/relay/fileconfig_test.go:19-32`): `listen.host 127.0.0.1`, a
       free `listen.port` chosen by binding port 0, `tls.mode off`, and one
       `hosts:` entry with a 16+ character secret;
     * self-check with `mcrelay paths --json --config $T\cfg\config.yaml`,
       which must exit 0. A non-private file would be refused by the relay's
       credential guard, so this proves the ACL.
   * **S2:** `mcrelay setup-service --binary $T\mcrelay.exe --service-config
     $T\cfg\config.yaml --force` exits 0, and its output contains no
     `systemctl`, `journalctl`, `loginctl`, `.service` or `make install`.
   * **S3:** within 30 s, `Get-ScheduledTask mcrelay` `State` is 4, and one
     `mcrelay.exe` process runs from the temp path.
   * **S4:** `mcrelay setup-service --refresh --json` reports verdict
     `unchanged`.
   * **S5:** `mcrelay setup-service` (no `--force`) exits 0 and reports
     unchanged (D14).
   * **S6:** kill the process (`Stop-Process -Force`), then expect `State` 4
     and a **new** process id within 120 s (D5).
   * **S7:** stop through the product path (`schtasks /change /disable` and
     `/end`, as D2 prints them), wait 90 s, and expect no `mcrelay.exe`
     process.
   * **S8:** `mcrelay setup-service --remove` exits 0, `Get-ScheduledTask
     mcrelay` is absent, and no process remains.
   * **finally:** remove the task if present, kill any stray process, delete
     the temp directories, then run the C3 `cmp`.
2. Output: one `PASS`/`FAIL` line per check, then `N CHECK(S) FAILED`. The exit
   code is the failure count.

**Verification (P11):**

```powershell
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts\acceptance-windows-service.ps1          # → 0 CHECK(S) FAILED
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\acceptance-windows-service.ps1    # → 0 CHECK(S) FAILED (5.1)
(Get-ScheduledTask -TaskName mcrelay -ErrorAction SilentlyContinue)                           # → nothing
```

Plus the Stability rule's `make ci-windows`. The script is not part of `make
ci-windows`: it is stateful, and MADR 0145's gate is not. It is added to the
AGENTS.md Windows gate list at the owner's direction, not by this plan.

**Release 1 boundary.** P1–P11 are complete. The owner cuts the release, then
runs the manual live update in Rollout.

### P12 — `--env` reaches the Windows daemon through a private file (D6; closes F9) — release 2

Starts only after release 1 has been published (C4, F18).

1. `internal/cli/envfile.go`:
   * `loadEnvFile(path string) error` reads `KEY=VALUE` lines. It rejects
     anything the existing `normalize` validation rejects (`setup.go:808-815`)
     and a file that is not owner-only (`appdirs.FileIsOwnerOnly`, the same
     posture as MADR 0155), then calls `os.Setenv` for each entry.
   * `internal/cli/serve.go` and `internal/relay/cli.go`'s serve command gain
     `--env-file`, applied first in `RunE`, before `config.Load`.
   * Tests cover valid, malformed, non-owner-only (Windows-only test), and
     precedence (a file entry overrides the inherited environment, as systemd's
     `Environment=` does).
2. `setup.go`:
   * remove P7's Windows `--env` refusal;
   * under `windows` with a non-empty `ExtraEnviron`, Setup writes
     `service.env` into the service config's directory (`EnsurePrivateDir` +
     `WriteFileAtomic(…, AtomicOptions{Perm: 0o600})`);
   * `serveArgs` appends `--env-file "<path>"`.

   `--remove` deletes the file.
3. `refresh_schtasks.go`: recovery accepts `--env-file` and reads the file back
   into `ExtraEnviron`. A missing file gives kept, with the reason.
4. Tests: render and recovery round-trip; the file is created and removed; C5
   holds for Unix.
5. `docs/ops-windows-install.md`: `--env` works on Windows through
   `service.env`.

**Verification (P12):** the Stability rule, plus P11's script extended by one
check in this commit: `--env MCREMOTE_LOG_LEVEL=debug` produces the file and
the argument, and `--refresh` reports unchanged.

### P13 — The task-launched daemon has no console (D7; closes F10) — release 2

Starts only after release 1 has been published **and** MADR/PLAN 0157 P4 (the
file sink) has been executed.

1. `internal/cli/detach_windows.go` (`//go:build windows`) implements
   `detachConsole()` as
   `windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole").Call()`.
   `x/sys/windows` has no `FreeConsole` wrapper; this is the call MADR probe 7
   measured. A "no console" result is ignored.
   `detach_other.go` is a no-op.
2. `serve` (both products) gains `--detach-console`, calling
   `detachConsole()` as the **first** statement of `RunE`, before logging
   setup. Only `serveArgs` passes it, on Windows. It is not documented as an
   interactive flag.
3. `schtasks.go` `serveArgs` appends `--detach-console`. `refresh_schtasks.go`
   recovery accepts it.
4. Tests: `serveArgs` includes it; recovery round-trips it; Unix renderings
   are unchanged (C5).
5. `scripts/acceptance-windows-service.ps1` adds **S3b**. After S3, snapshot the
   `OpenConsole`, `conhost` and `WindowsTerminal` processes before and after
   `schtasks /run` (the MADR probe-7 method, sampled every 50 ms for 3 s), and
   expect no new host whose parent chain leads to the task's process.
6. **The unverified piece is the real logon.** This is a manual owner step,
   recorded in the Execution record. After release 2 is installed, log off and
   back on. Then run the probe-7 window enumeration against the live daemon:
   expect no `PseudoConsoleWindow` owned by it, no `conhost` child, and no
   Windows Terminal started within 2 s of the daemon.

   If a console host still appears, stop. The fallback is a GUI-subsystem
   service binary (MADR probe 7 row 2). That fallback is a new decision, not
   part of this plan.
7. `docs/ops-windows-install.md`: no console at logon, and the log file is the
   place to look (MADR 0157).

**Verification (P13):** the Stability rule, P11's script with S3b, and step
6's recorded result.

## Verification (whole plan)

After P11 (release 1), and again after P13 (release 2), from the repository
root:

```powershell
$env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'; go vet ./...; Remove-Item Env:GOOS,Env:GOARCH
go test ./internal/admin/ -run TestSocketIdentityStable -v                 # PASS
go test ./internal/fsutil/ -run OwnerOnly -v                               # PASS
make ci-windows                                                            # ALL SELECTED CHECKS PASSED
pwsh -File scripts\acceptance-windows.ps1                                  # only the 0157-owned log_dir check fails, until 0157 P1
pwsh -File scripts\acceptance-windows-service.ps1                          # 0 CHECK(S) FAILED
mcremote setup-service; mcremote setup-service                             # second: unchanged, exit 0 (C3 cmp)
$env:USERNAME='bogus'; mcremote setup-service --print-only | Select-String UserId   # the SID
git grep -n 'MC_WINDOWS_SIGN' -- docs/ops-windows-install.md                # "not yet" wording only
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | Windows vet clean; `make ci-windows` green after every phase | (§1), C1 |
| A2 | `TestSocketIdentityStable` and the owner-only atomic-write test PASS, not SKIP | D4, D12 (§2) |
| A3 | A second `setup-service` reports unchanged and writes nothing | D14 (§3) |
| A4 | `--refresh --json` gives a verdict: `unchanged` on a current task, `refreshed` on the v0.17.4 shape | D1 (§4) |
| A5 | The Windows summary names no Unix command | D2 (§5) |
| A6 | The service lifecycle gate passes, including relaunch within 120 s and a clean `--remove` | D5, D9, D11 (§6) |
| A7 | The principal is the SID whatever `USERNAME` says; `--unit-name other` errors | D8, D13 (§8) |
| A8 | `acceptance-windows.ps1` parses under PowerShell 5.1 and 7 | D11 (F16) |
| A9 | No document claims an unwired signing hook; 0116 amended additively | D10 (§9) |
| A10 | `--env` produces a private `service.env` and a working `--env-file` (release 2) | D6 |
| A11 | No console host for the task-launched daemon, at `schtasks /run` and at a real logon (release 2) | D7 (§7) |
| A12 | The live `mcremote` task is byte-identical before and after every automated check | C3 |

**A12 is the criterion most likely to be dropped quietly.** Nothing fails
when it is violated, because the daemon keeps running. A phase that
"just re-registers to test it" leaves no trace, except in the `cmp` that C3
requires.

## Rollout and Rollback

**Release 1 (after P11).** The owner cuts a release. Then, on this host, with
the owner's explicit go-ahead because it replaces the live daemon:

1. Run `mcremote update` from the installed v0.17.4. Expected:
   * download, verify, stop (the v0.17.4 lifecycle does `/end` only; the new
     disable/enable arrives with the new binary);
   * swap, then run the **new** binary's `setup-service --refresh --json`,
     which gives `refreshed` (it adds P9's trigger);
   * start, and `WaitHealthy` succeeds;
   * commit.
2. Confirm:
   * `Get-ScheduledTask mcremote` `State` 4;
   * the registered XML has both triggers;
   * `mcremote setup-service --refresh --json` gives `unchanged`;
   * `mcremote version` reports release 1.
3. Record the whole transcript in the Execution record.

**If step 1 fails after the refresh (F18):** the v0.17.4 binary's rollback
cannot restore a Task Scheduler definition. The binary is rolled back, and the
refreshed definition remains. Release 1's arguments are identical to v0.17.4's
(C4), so the old binary still runs under it. Confirm with `Get-ScheduledTask`
`State` 4. If it does not, run `mcremote setup-service --force` with the
rolled-back binary, which re-registers that binary's own definition.

**Release 2 (after P13).** The same update procedure, now from release 1,
whose rollback **can** restore a task (P10). Then step 6 of P13, the logon
check.

**Per-phase revert.** Each phase is one commit; `git revert` restores that
slice. After release 1 is published, do not revert P10 alone: hosts that
updated rely on its restore path.

## Deferred (named, so they are not mistaken for oversights)

* **SCM / Windows Service, boot-start, and headless mcrelay on Windows.** MADR
  0116's decisions, and option B's reasons.
* **Restart latency under a minute.** The task engine's minimum repetition is
  one minute (MADR probe 8); a faster watchdog would need a resident helper.
* **Honouring custom unit and task names in every platform's lifecycle** (F17
  covers only the Windows refusal).
* **A real test of `schtasks` output under a non-English UI.** D9 removed the
  dependency, so it is no longer needed for correctness.
* **Provider CLIs that only a profile script puts on `PATH`** (nvm, fnm, volta,
  scoop). They are unverified on this host, and none is installed. If one
  appears, `--env PATH=…` (P12) is the escape hatch.
* **The three load-sensitive test timeouts** (probe 12). Each passed 3/3 alone
  and they are not in `ci-flakes.tsv`. They belong in the flake ledger's
  process, not here.
* **Adding `acceptance-windows-service.ps1` to `make ci-windows` or
  AGENTS.md.** The owner's call (P11).
* **Automated `update` of the live mcremote task.** Deliberately manual (C3).
* **Correcting MADR 0157's "no console" sentence.** In 0157's next revision.

## Execution record

Not yet executed.
