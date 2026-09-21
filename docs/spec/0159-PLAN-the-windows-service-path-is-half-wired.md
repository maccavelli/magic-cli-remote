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
  `internal/admin/admin.go` (the two `socketIdentity` call sites only), and, by
  the 2026-09-19 execution amendment, `internal/admin/owner_unix.go` and
  `internal/admin/owner_unix_test.go` (the signature change only).
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

1. Change the signature on both platforms to `socketIdentity(path string, fi
   fs.FileInfo) (uint64, bool)`, and pass the socket path from the two callers
   in `admin.go` (`:129-134`, `:146-150`). Unix ignores `path` and keeps its
   inode logic unchanged; its test call sites pass the path. This was an
   owner-approved scope amendment, 2026-09-19: Windows can only reach a file
   index by opening the path. The Windows implementation:
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

### P1–P11: release 1 code complete (2026-09-19)

The pair was accepted and committed in `3734c4e`. One owner-approved scope
amendment, for P3, is `e871a9a`. The phase commits:

| Phase | Commit | Result |
| --- | --- | --- |
| P1 | `cfa0da0` | `acceptance-windows.ps1` parses (0 errors in 5.1 and 7). A run: every check PASS except `paths --json (C1)` `log_dir` (owned by 0157 P1), as planned. |
| P2 | `4ec5f70` | The ops page no longer names `MC_WINDOWS_SIGN` at all, which is stricter than the plan's check expected. The 0116 amendment is purely additive (0 lines removed). |
| P3 | `844a667` | `TestSocketIdentityStable` and the new `TestSocketIdentityOfALiveSocket` PASS, not SKIP. The flags in step 1 worked first time. |
| P4 | `1340f28` | `TestWriteFileAtomicIsOwnerOnlyInPrivateDir` PASS, so the MADR's [unverified] D12 is now verified. Teeth check: the same test against a non-private directory fails ("readable by another principal"). |
| P5 | `4abb1d5` | Semantic comparison. The scrubbed v0.17.4 export equals the render once the account is resolved, and 10 field changes are each detected. |
| P6 | `214a8fe` | The principal comes from the token SID. `USERNAME=bogus` gives the SID. **Live: `setup-service` (no `--force`) exits 0 and reports unchanged, and the task is byte-identical (C3). F19 is fixed on the real task store.** |
| P7 | `fc81ce2` | Windows summary; `--unit-name` and `--env` refused. The Unix goldens were captured before editing and are unchanged (C5). |
| P8 | `1379ce5` | `Get-ScheduledTask` state probe. Live `doctor`: present, loaded, active. |
| P9 | `698f159` | Watchdog trigger; stop = disable + end; start = enable + run. **Live, with the real render and its fixed past boundary: relaunches at 09:21:01, 09:22:01, 09:23:01, and 0 runs in 90 s once disabled.** The MADR's [unverified] D5 boundary is now verified. |
| P10 | `be5a575` | `--refresh` on Windows, and restore of a refreshed task. Live `--print-only` shows the definition a refresh would register, with the arguments identical to v0.17.4's (C4); the task is unchanged (C3). |
| P11 | `e5c514f` | `acceptance-windows-service.ps1`: **ALL CHECKS PASSED under PowerShell 7 and 5.1** (S1–S8 plus C3), with nothing left behind. |

Every Go phase ended with the full Stability rule on Windows (`make ci-windows`
ALL SELECTED CHECKS PASSED, `go test` 41 `ok`, race clean) and in WSL, except
where noted below. C3 was checked with a before-and-after `cmp` at every live
step, and the live mcremote task was never modified. C4 holds: the rendered
`<Arguments>` equal v0.17.4's after P9 and after P10.

**What the plan predicted incorrectly.**

1. **P3's scope.** The signature change reaches `owner_unix.go` and its test.
   The owner approved this; the amendment is `e871a9a`.
2. **P5's sequencing.** The plan backed the principal comparison with
   `windows.LookupSID`, which needs a Windows-only file outside P5's scope.
   Instead, the comparison seam defaults to a case-insensitive match, and P6
   renders the principal as the SID, the form Task Scheduler stores, so a live
   task matches with no lookup. Consequences:
   * P5's live check refused, as expected, and wrote nothing;
   * "unchanged" went live in P6;
   * P5 did not need `schtasks.go`.
3. **A test that only Linux could catch.** `TestSetupDispatchesToWindows`
   failed in WSL after P6. Off Windows there is no token to read, so the test
   must inject the principal seam. This is in P6's scope, and no assertion
   changed. The Windows run could not show it, because the real token lookup
   succeeds there. This is the case the WSL half of the Stability rule exists
   for.
4. **`setup-service --binary` does not clean its path.** Passing
   `$LOCALAPPDATA/Programs/...` from Git Bash registers a `Command` with mixed
   slashes, which then never equals the live task. It works, but it is not
   byte-identical. Not fixed here (see Deferred).
5. **P7's refusal test was placed wrongly.** `setup_test.go` is
   `//go:build unix`, so the test moved to the untagged
   `result_print_test.go`, where it runs on every platform. The summary also
   says "registered in Task Scheduler", because on the unchanged path no
   `/create` runs.
6. **P8 added `-TaskPath '\'`** to the measured probe, so a same-named task in
   another folder cannot be matched. Probe 11 did not measure this variant; the
   live `doctor` run and the real "absent" test prove it.
7. **P9's tests needed restructuring, not just updating.**
   * `TestStopMapsToEnd` pinned the old behaviour. It is replaced by
     `TestStopDisablesThenEnds` (full call sequence; running, ready and absent)
     plus `TestStartEnablesThenRuns`.
   * The P5 field test would have become vacuous, because the v0.17.4 base
     already differed from the new render. It now starts from a simulated
     current-shape export, and **asserts that base equals the render** before
     testing each change.
   * The "watchdog removed" case removes the whole block. Renaming the element
     would only have caused a parse error.
8. **P10's expected live output was wrong.** `--refresh --print-only --json`
   prints the definition the refresh would write, not a JSON verdict. That is
   the existing behaviour on every platform (`setup_service.go`), where
   `--print-only` wins. The verdict for this v0.17.4 shape is proven by
   `TestRefreshSchtasksRefreshesTheV0174Task` against the scrubbed export of
   this very task. A real, writing refresh ran in P11's S4, against the
   isolated mcrelay task.
9. **P11's first run found a bug in the script itself, not in the product.** A
   PowerShell function returning an empty array unrolls it to `$null`, and
   under StrictMode `.Count` then throws. It was fixed with `return , @(...)`
   before commit. The failed run still cleaned up completely (no task, no
   process, no directories), which exercised the `finally` block.
10. **A fourth load-sensitive test.**
    * `internal/ws` `TestV2QuietConnectionSurvivesOnPongs` failed in 2 of about
      10 full WSL runs, with `read_deadline`.
    * Alone it passed 5/5, and the package passed 3/3.
    * `go list -test -deps ./internal/ws` contains no `internal/cli/service`,
      so no 0159 change can affect it.
    * The pre-0159 baseline `38078c5` passed 2/2, and the P9 tree passed 3/3
      more.

    It is not one of the three the Stability rule names. It is recorded here,
    not waived silently.
11. **The pipe export is 8-bit text** (MADR 0116 F26). A task path containing
    non-ASCII characters would therefore reach `sameTaskDefinition` and the
    refresh backup as mis-decoded bytes. The consequence is **[unverified]**:
    no non-ASCII path exists on this host. It would show as a harmless
    "refreshed", and a restore could corrupt the path. See Deferred.

**Not yet done.**

* **Release 1** is an owner action: cut it, then run the manual live update in
  Rollout.
* **P12 and P13** wait for release 1 (C4, F18). P13 also waits for MADR 0157
  P4.
* The setup summary still says to update with `install.ps1` only; `update`
  works on Windows only once release 1 is installed. Adding `<product> update`
  to the Windows summary is a one-line follow-up for P12.

Additional deferred items, found during execution:

* Clean `setup-service --binary` (and `--service-config`) paths with
  `filepath.Clean` (item 4).
* Read task exports in a lossless encoding, for example PowerShell
  `Export-ScheduledTask`, before trusting refresh or restore with non-ASCII
  paths (item 11).
* Add `TestV2QuietConnectionSurvivesOnPongs` to the flake ledger's process
  alongside the other three (item 10).

## Amendment — 2026-09-19: P13 ships next, in v0.18.1, and P14 is added

Owner decision, after release 1 was published as **v0.18.0** and installed on
this host (MADR amendment of the same date: F20, F21, D15, and D7's changed
ordering). Where this section and the sections above disagree, this section
wins.

**Order.** P13 no longer waits for MADR/PLAN 0157 P4 or for P12. P13 and the
new P14 ship together in **v0.18.1**. P12 follows in a later release, unchanged.

```text
[release 1 = v0.18.0] ──► P13 ──► P14 ──► [v0.18.1] ──► P12 ──► [release 2]
```

C4 is satisfied: release 1 is published, and v0.18.0's rollback can restore a
Task Scheduler definition (P10), so an update from v0.18.0 that fails after its
refresh restores the old task.

**P13 scope, corrected.** Step 3 edits refresh recovery, which the original
scope list omitted. P13 may also touch `internal/cli/service/refresh_schtasks.go`
and `internal/cli/service/refresh_schtasks_test.go`.

**P13 step 7, changed.** `docs/ops-windows-install.md` says the task-launched
daemon has no console, and that until MADR 0157 lands its log output is not
kept anywhere: run `mcremote serve` in a terminal to see it. It also says a
task written by v0.18.1 names `--detach-console`, which v0.18.0 and earlier
reject. After a manual downgrade, `mcremote setup-service --force` with the
older binary re-registers a task it can run.

**P13 step 6 happens in the v0.18.1 rollout**, not release 2's.

### P14 — The installer names every folder it installs (D15; closes F20)

1. `scripts/install.ps1`: call `Add-ToPathNotice` for each of `$Products`, in
   a `foreach` over `$Products`, replacing the single `mcremote` call. The
   printed advice appends to
   `[Environment]::GetEnvironmentVariable('Path', 'User')`, not `$env:Path`.
2. `scripts/install_ps1_unit_test.ps1`:
   * load `Add-ToPathNotice` from the real script's AST, as the file already
     does for `Select-ManifestEntry`;
   * call it for a folder that is not on the User `Path`, capture its
     output, and check that the output names the folder and that the advice
     reads the User `Path`;
   * add a static check that the script's only `Add-ToPathNotice` call is
     inside a `foreach` over `$Products`.

**Scope (P14):** `scripts/install.ps1` (`Add-ToPathNotice` and its call only),
`scripts/install_ps1_unit_test.ps1`.

**Verification (P14):** both installer tests pass under PowerShell 5.1 and 7:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1
pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_unit_test.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell powershell
pwsh       -NoProfile -ExecutionPolicy Bypass -File scripts/install_ps1_test.ps1 -Shell pwsh
```

Plus the Stability rule.

**Acceptance (additions).**

| # | Criterion | MADR |
| --- | --- | --- |
| A11 | (moved to v0.18.1) No console host for the task-launched daemon, at `schtasks /run` (S3b) and at a real logon (owner) | D7 |
| A13 | The installer prints a PATH notice for every product folder that is not on the User `Path`, with advice that reads the User `Path` | D15 |

### P15 — The installer puts its folders on the User Path (D16; second amendment, 2026-09-19)

Added by the owner's decision of 2026-09-19. It ships in v0.18.1 with P13 and
P14.

1. `scripts/install.ps1`:
   * `-NoPathUpdate` switch; `MCREMOTE_INSTALL_NO_PATH_UPDATE=1` has the same
     effect.
   * `Add-ToUserPath -Dir <d> [-Key <registry path>]`:
     * `-Key` defaults to `HKCU:\Environment`, and exists so the unit test can
       use a scratch key;
     * reads `Path` unexpanded, and appends `<d>` unless an entry equals it
       once expanded, trimmed of a trailing `\`, and compared case-blind, or the
       Machine `Path` already has it;
     * writes back with the original value kind (`ExpandString` if absent);
     * adds `<d>` to `$env:Path`;
     * returns whether it changed anything.
   * `Send-EnvironmentChange` broadcasts `WM_SETTINGCHANGE` through
     `SendMessageTimeout`, using `Add-Type` P/Invoke with a 5 s timeout. A
     failure is a warning, not an error.
   * Main: for each product, `Add-ToUserPath`, unless opted out, in which case
     `Add-ToPathNotice`. Broadcast once if anything changed. Under `-WhatIf`,
     log "would add … to the User Path".
2. `scripts/install_ps1_unit_test.ps1` (U14), against
   `HKCU:\Software\mcremote-install-unit-<guid>`, removed in `finally`:
   * absent → appended, and a second call returns false with the value
     unchanged;
   * a `REG_EXPAND_SZ` value containing `%USERPROFILE%\x` keeps both its kind
     and the literal `%USERPROFILE%\x`;
   * `<d>\` and an upper-cased `<d>` count as present;
   * a missing value is created as `ExpandString`;
   * a static check: the main block's `foreach` over `$Products` calls
     `Add-ToUserPath`, and that call sits behind the opt-out;
   * the U13e static check is updated to accept the notice in the opt-out
     branch.
3. `scripts/install_ps1_test.ps1`: the child process gets
   `MCREMOTE_INSTALL_NO_PATH_UPDATE=1` in every mode. A new check compares
   the real `HKCU\Environment` `Path` (raw, and its kind) before and after the
   whole run.
4. `docs/ops-windows-install.md` (Install): the installer adds both folders to
   the User `Path`, and explains how to opt out.

**Scope (P15):** `scripts/install.ps1`, `scripts/install_ps1_unit_test.ps1`,
`scripts/install_ps1_test.ps1`, `docs/ops-windows-install.md` (the Install
section).

**Verification (P15):** P14's four commands, plus:
* a mutation check: reading `Path` expanded, or writing it as `String`, fails
  U14;
* the Stability rule's `make ci-windows`.

The real User `Path` of this host is checked unchanged after the test runs.

**Acceptance (addition).**

| # | Criterion | MADR |
| --- | --- | --- |
| A14 | A default install puts both product folders on the User `Path`, preserves `%VAR%` entries and the value kind, and is idempotent. The opt-out restores the notice. | D16 |

**Rollout (v0.18.1).** The owner cuts v0.18.1. Then, on this host, with the
owner's go-ahead:

1. Run `mcremote update -y` from v0.18.0. The new binary's refresh adds
   `--detach-console` to the task (`refreshed`). The restart opens no window.
2. Confirm:
   * `State` 4;
   * the task's arguments end with `--detach-console`;
   * `setup-service --refresh --json` gives `unchanged`;
   * no `conhost` child of the daemon.
3. Log off and on (P13 step 6), then repeat the `conhost` check.
4. Record the results in the Execution record.

### P13 and P14: v0.18.1 code complete (2026-09-19)

Release 1 was published as **v0.18.0** (CI run `35461886119`, 10 of 10 jobs
green, 12 assets). The owner updated both products on this host, which
produced the amendment above. The amendment is `2825289`.

| Phase | Commit | Result |
| --- | --- | --- |
| P13 | `43bd535` | `serve --detach-console` in both products; the task renders it last, and refresh recovers it. `acceptance-windows-service.ps1` **ALL CHECKS PASSED under PowerShell 7 and 5.1**, including the new S3b and C3. |
| P14 | `9511991` | The PATH notice runs for every product, and its advice appends to the User `Path`. The unit test passes 41/41 and the e2e test 43/43, each under 5.1 and 7. |

**S3b has teeth.** A throwaway task (C7) ran the same mcrelay build, once
without the flag and once with it, sampled by the S3b method:

```text
without flag: daemon processes=1; new console hosts=2 OpenConsole.exe pid 47180 parent 2580; conhost.exe pid 43408 parent 25436
with flag:    daemon processes=1; new console hosts=0
```

The task was removed afterwards. Two mutations of `install.ps1` also fail U13:
restoring the single `mcremote` call fails U13e, and restoring the `$env:Path`
advice fails U13b and U13c.

The Stability rule held for both phases:

* `make ci-windows`: ALL SELECTED CHECKS PASSED;
* `make pre-add-check`: 9 Go files clean;
* WSL: `go vet` clean, `go test ./...` 41 `ok` and no failures, `-race` on
  `internal/cli` and `internal/cli/service` clean.

**What the amendment predicted incorrectly.**

1. **Where the detach call lives.** The plan put it in `internal/cli`, but
   `internal/cli` imports `internal/relay`, so mcrelay's serve cannot import
   it. It is `internal/cli/service/detach_{windows,other}.go`, beside the task
   renderer; both products already import that package. The exported
   `DetachConsoleFlag` constant names the flag once for serve, render and
   recovery.
2. **P13's scope missed `task_compare_test.go`.** Its fixtures model the
   current render (`currentShapeExport`) and the v0.17.4 render
   (`renderV0174Shape`), so adding an argument broke three tests. No assertion
   was weakened:
   * the helpers add and strip the flag;
   * a new `v0180ShapeExport` models the v0.18.0 task;
   * the field-detection test gains a "detach removed" case;
   * `TestRefreshSchtasksAddsTheDetachFlagToAV0180Task` proves the v0.18.1
     update path.
3. **A latent bug in the P11 script.** `Get-RelayProcess` returns its array as
   one pipeline object, so `Get-RelayProcess | ForEach-Object { $_.ProcessId }`
   reads `.ProcessId` from the array itself. When no process is running, that
   fails under StrictMode. The `finally` cleanup had this form; the teeth probe
   found it. Both uses are now `foreach`.
4. **Windows PowerShell 5.1 launched from Git Bash.** It inherits PowerShell
   7's `PSModulePath` and cannot find `Get-FileHash`, so the e2e test dies
   before it starts. This is a host effect, not a product one: with
   `PSModulePath` cleared it passes 43/43. CI launches each shell directly.
5. **The WSL half caught an incomplete patch.** `git diff HEAD` omits
   untracked files, so the first WSL run built without `detach_*.go` and
   failed. The fix is to mark new files intent-to-add before exporting the
   patch.

**P15 (`781420f`), added by the second amendment (`153c5da`).** The installer
adds both folders to the User `Path` (D16).

* **Unit test:** 61/61 under 5.1 and 7, with 20 new U14 checks against a
  scratch key, which is removed afterwards.
* **Mutations:** three, each caught.
  * Reading `Path` expanded fails U14 and U14b: the `%VAR%` entry comes back
    literal.
  * Writing `String` fails U14 and U14e.
  * Dropping the opt-out branch fails U14f.
* **e2e:** 44/44 under both shells. The new check "the real User Path is
  unchanged by the whole run" passes, and an independent before-and-after read
  of this host's `HKCU\Environment` `Path` (kind and raw value) was identical.
* **Broadcast:** `Send-EnvironmentChange` was run for real under 7.6.6 and
  5.1.26100. The type loaded, there were no warnings, and it took 390 ms and
  411 ms.
* `make ci-windows` passed.

Not covered by an automated test: the real write to `HKCU\Environment`, since
any test would change the tester's own `Path`. What is tested is the same
function against a scratch key, plus the e2e proof that the real key is left
alone under the opt-out.

**Not yet done.**

* **v0.18.1** is an owner action: cut it, then follow the amendment's Rollout.
  That covers the update from v0.18.0, the check that no window opens, and the
  log off and on for P13 step 6. **A11's logon half stays open until then.**
* P12 follows in a later release, unchanged.

## Amendment — 2026-09-19 (third): P16–P21 ship in v0.19.0, and the scope widens to F24, F28 and F29

Owner report and owner instruction, 2026-09-19 (MADR amendment of the same date,
third: F22–F37, D17–D26). Where this section and the sections above disagree,
this section wins.

**What the v0.18.1 rollout showed.** `mcremote update -y` opened **no window** —
the `schtasks /run` half of A11 now passes on the real host, not only in S3b.
Verified alongside it: the task is `Running`/`Enabled` as `macsm`, its arguments
end with `--detach-console`, `setup-service --refresh --json` reports
`unchanged`, and the daemon (PID 18572, parent `svchost.exe`) has **no child
processes at all**.

**What it then showed.** Starting an agent session opened a window, and closing
that window killed the session. P13 is correct and is not reverted; the window
belongs to the provider CLI now (F22). Investigating it surfaced F24, F28 and
F29, and the owner widened the scope to all three.

```text
[v0.18.1] ──► P16 ──► P17 ──► P18 ──► P19 ──► P20 ──► [v0.19.0] ──► P12 ──► [release 3]
```

**A11's logon half stays open** and is folded into the v0.19.0 rollout, which
ends with the same check.

### Scope (additions — the only files P16–P20 may touch)

```text
P16  internal/procutil/procutil_windows.go        D17, D18: the constructor, the flag, the cached console state
     internal/procutil/procutil_unix.go           D18: the constructor, no flags
     internal/procutil/procutil_other.go          D18: the constructor, no flags
     internal/procutil/command.go           (new) D18: the portable doc comment and the shared seam
     internal/procutil/procutil_windows_test.go   D20 helper-process test, creationFlags table
     internal/procutil/procutil_test.go           D18 constructor behaviour, all platforms
P17  internal/provider/codex/diagnostics.go       D19
     internal/provider/codex/managed_daemon.go    D19
     internal/provider/codex/sandbox_health.go    D19
     internal/provider/codex/provider.go          D19 (the --version probe and the engine spawn)
     internal/provider/codex/adapter.go           D19
     internal/provider/codex/auth.go              D19
     internal/provider/codex/logout.go            D19
     internal/provider/codex/store_reality.go     D19
     internal/provider/acpagent/acpagent.go       D19
     internal/provider/acpagent/terminal.go       D19
     internal/provider/httpagent/provider.go      D19
     internal/providerauth/cli.go                 D19
     internal/tailnet/tailnet.go                  D19
     internal/cli/service/setup.go                D19 (runCmd, runCmdOutput)
     internal/cli/service/exec_refresher.go       D19
     internal/procutil/spawnsites_test.go   (new) D19 static ban and its reasoned allowlist
P18  internal/procutil/procutil_windows.go        D22: attach, signal, detach, serialized
     internal/procutil/procutil_windows_test.go   D22 tests
     internal/cli/service/detach_windows.go       D22: notify procutil that the console is gone
     internal/cli/service/detach_other.go         D22: unchanged signature
P19  internal/provider/launch/launch.go           D23, D25: errors, limits, doc correction
     internal/provider/launch/launch_windows.go   D23: quoting, CmdLine, cmd /d /s /v:off /c
     internal/provider/launch/launch_unix.go      D23: unchanged behaviour, shared signature
     internal/provider/launch/launch_windows_test.go  D23 cases
     internal/provider/launch/launch_test.go      D23 portable cases
     internal/provider/codex/provider.go          D24: spawn through launch.Command
     internal/provider/acpagent/acpagent.go       D24: same
     internal/provider/httpagent/provider.go      D24: same
P20  docs/ops-windows-install.md                  D26
```

Out of scope, named so the boundary is not mistaken for an oversight: resolving
an npm shim to its real target (which is what would make `%` representable),
P12's `--env` work, and any change to the task definition. All in Deferred.

### Stability rule (unchanged, and it now matters more)

Every phase ends with `make pre-add-check FILES=...` on its Go files, then
`make ci-windows`, then `go test -race ./internal/...` for the packages it
touched. P17 and P19 additionally run the WSL Linux lane, because both change
files that compile on Linux. `git push` and tags still need an explicit
instruction in the same turn.

### P16 — The constructor, and no window for a console-less parent's children (D17, D18, D20; closes F22, F23, F25)

1. `internal/procutil/command.go` (new, portable): declare
   `func Command(ctx context.Context, name string, args ...string) *exec.Cmd`
   with the doc comment stating that it is **the** way a child process is built
   in this repository, that it applies the process group and, on Windows, the
   D17 window flag, and that D19's test enforces its use. It delegates to an
   unexported `newCommand` per platform.
2. `procutil_unix.go`, `procutil_other.go`: `newCommand` is
   `exec.CommandContext` plus `SetProcessGroup`. No behaviour change.
3. `procutil_windows.go`:
   * `hasConsole() bool` via a `GetConsoleProcessList` lazy proc — F36 measured
     that `x/sys` does not export it. A zero return means no console. **Not
     `GetConsoleWindow`**: D17 records the measurement that rules it out.
   * `consoleState()` caches the answer behind a `sync.Once`, with a doc comment
     giving D22's reason for caching rather than sampling: an attachment made
     while signalling must not be observed by a concurrent spawn.
   * `creationFlags(hasConsole bool) uint32` returns
     `windows.CREATE_NEW_PROCESS_GROUP`, plus `windows.CREATE_NO_WINDOW` when
     false. Both constants exist in `x/sys` v0.47.0 (F36), so declare no local
     constant.
   * `newCommand` builds the command, then applies `creationFlags(consoleState())`.
   * `SetProcessGroup` keeps its current body and name (D18) and is now only for
     callers holding a command they built another way.
4. `procutil_test.go`: `Command` returns a runnable command on every platform;
   `Path`, `Args` and `SysProcAttr` are set as expected; a `nil` context is
   rejected the way `exec.CommandContext` does.
5. `procutil_windows_test.go`:
   * `creationFlags` table: with a console, `CREATE_NO_WINDOW` is **absent**;
     without, present. The with-console row is the one that protects the
     interactive path — it asserts an absence, and A16 exists so it is not
     deleted as redundant.
   * the D20 integration test, three processes deep, because two is not enough:
     the test process cannot be assumed to lack a console. Roles come from an
     environment variable and re-execute the test binary (`TestHelperProcess`
     idiom): role `parent` calls `FreeConsole`, builds the child with
     `procutil.Command` — the real path, not a copy — starts it and relays its
     output; role `child` prints `GetConsoleWindow()`. Assert `0`. Skip with a
     stated reason if the helper cannot be re-executed; never skip silently.

**Verification (P16).**

```powershell
go test ./internal/procutil/ -run 'Command|Console|CreationFlags' -v
go test -race ./internal/procutil/
git stash ; go test ./internal/procutil/ -run Console ; git stash pop   # must FAIL pre-fix
```

Mutations that must be caught: replacing `hasConsole()` with
`GetConsoleWindow() == 0` fails the with-console row; dropping
`CREATE_NO_WINDOW` fails the D20 test; sampling live instead of caching is
covered by P18's concurrency test, not here.

### P17 — Every child process is built by the constructor (D19; closes F28)

1. Replace `exec.Command`/`exec.CommandContext` with `procutil.Command` at every
   site in the P17 scope list, deleting the now-redundant `SetProcessGroup` call
   that follows 11 of them. Where a site holds a `context`, pass it; where it
   does not (`acpagent.go:424`, `httpagent/provider.go:502`,
   `providerauth/cli.go:94`, `codex/provider.go:530`), pass
   `context.Background()` and leave the existing lifetime mechanism alone —
   changing cancellation semantics is not this phase's job.
2. `internal/tailnet/tailnet.go`: the `execCommand` seam keeps its signature and
   calls `procutil.Command`, so the test stub is unaffected.
3. `internal/cli/service/setup.go`: `runCmd` and `runCmdOutput` build through
   the constructor, which covers all 12 `runSchtasks` call sites at once.
4. `internal/procutil/spawnsites_test.go` (new): walk every non-test `.go` file
   under `cmd/` and `internal/`, parse with `go/ast` rather than by regex, and
   fail on any `exec.Command` or `exec.CommandContext` call outside
   `internal/procutil`. The allowlist is a map in the test file from path to
   reason; seed it only with what genuinely cannot route through the
   constructor:
   * `internal/updateclient/codesign_darwin.go` — darwin-only `codesign`,
     never a daemon child on Windows;
   * `internal/provider/launch/launch_windows.go` and `launch_unix.go` — they
     build the command the constructor cannot (D23's `CmdLine`), and P19 makes
     them call the constructor first.
   The test fails if an allowlist entry has an empty reason, and fails if an
   allowlisted path no longer contains a matching call — so the list cannot rot.

**Verification (P17).**

```powershell
go test ./internal/procutil/ -run SpawnSites -v
go build ./... ; go vet ./...
go test ./internal/provider/... ./internal/providerauth/ ./internal/tailnet/ ./internal/cli/service/
make ci-windows
```

WSL lane: `go build ./... && go test ./internal/...` on Linux, since every file
in this phase compiles there. Mutation: reintroduce one bare `exec.Command` and
the test must name that file and line.

### P18 — A console-less daemon stops a child politely (D22; closes F24, pins F32)

1. `internal/procutil/procutil_windows.go`:
   * lazy procs for `AttachConsole` and `FreeConsole` (F36).
   * `var consoleMu sync.Mutex` guarding the borrow, with a comment naming what
     is shared: console attachment is process-wide.
   * `signalBreak(pid uint32) error`:
     * if `consoleState()` says we have a console, call
       `windows.GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid)` — today's path,
       for `mcremote serve` in a terminal;
     * otherwise take `consoleMu`, `AttachConsole(pid)`, and on success
       `defer FreeConsole()` before `GenerateConsoleCtrlEvent`, so no early
       return can leave a borrowed console attached.
     * `AttachConsole` failing — the child already exited, or has no console —
       returns the error and changes nothing.
   * `TerminateProcessGroup` calls `signalBreak` in place of its direct
     `GenerateConsoleCtrlEvent`, keeping its existing escalation and its
     `bool` contract unchanged: `true` iff the polite phase sufficed.
2. `internal/cli/service/detach_windows.go`: after `FreeConsole`, call the new
   `procutil.NoteConsoleDetached()` so the cached state is authoritative even if
   something sampled it earlier. Keep `DetachConsole` where it is — moving it
   into `procutil` is tempting but `internal/cli/service` is where P13 put it and
   the import direction is already proven; the note is a one-line seam instead.
   `detach_other.go` gains the same no-op for signature parity.
3. `procutil_windows_test.go`:
   * the polite-stop test, built on P16's helper-process harness: role `parent`
     calls `FreeConsole`, starts a child that notifies on **SIGINT** (F35 — there
     is no `syscall.SIGBREAK` on Windows, and CTRL_BREAK arrives as SIGINT),
     calls `TerminateProcessGroup`, and asserts it returned **true**, that the
     child exited with the code it uses for a clean drain, and that **the parent
     is still alive afterwards** — probe 11's most important observation, and the
     one a reader would never think to check.
   * a concurrency test: two children, `TerminateProcessGroup` on both from
     separate goroutines, run under `-race`; both must report `true`, which is
     what the mutex buys.
   * an already-exited child: `signalBreak` must return an error and
     `TerminateProcessGroup` must still report the process gone rather than hang.

**Verification (P18).**

```powershell
go test ./internal/procutil/ -run 'Terminate|Polite|Concurrent' -v
go test -race ./internal/procutil/ -count 2
```

Mutation: drop the mutex and the concurrency test under `-race` must fail or
flake visibly; drop `defer FreeConsole()` and the polite-stop test's follow-up
assertion — that the parent reports no console afterwards — must fail.

### P19 — Batch arguments are quoted, refused only when unrepresentable, and the guard is actually reached (D23, D24, D25; closes F29, F30, F31, F34, F37)

1. `internal/provider/launch/launch.go`:
   * correct the package doc: `CreateProcess` starts a batch file directly and
     supplies `cmd.exe` **implicitly** (F33, probe 12), so routing through
     `cmd.exe` is how the interpreter is controlled, not how it is reached, and
     the guard must apply either way.
   * replace `ErrUnsafeBatchArgs`'s message to name the character and say it
     cannot be represented rather than that it is unsafe.
   * split the ceiling (D25): `maxCommandLineNative = 32767` (CreateProcessW)
     and `maxCommandLineBatch = 8191` (cmd.exe), each with its source in a
     comment, and `ErrCommandLineTooLong` reporting which applied.
2. `internal/provider/launch/launch_windows.go`:
   * delete `safeBatchChars` and `unsafeBatchChar`'s allowlist. Refuse exactly
     `"`, `%`, `\r` and `\n`, each with its reason in the error (D23), and
     accept everything else.
   * build the batch command line explicitly:
     `comspec + " /d /s /v:off /c " + quoted(shim, args...)`, where `quoted`
     wraps the whole run in one outer pair and each element in its own quotes,
     matching probe 13's measured shape exactly. Set it on
     `cmd.SysProcAttr.CmdLine` and leave `cmd.Args` informative only — this is
     the workaround Go's own issues point to (#69939).
   * a trailing backslash inside a quoted element is the one shape probe 13
     showed passing through oddly; add a test case for it and, if the batch
     receives it altered, refuse it too rather than guessing.
   * call `procutil.Command` to build the command before setting `CmdLine`, so
     the D17 flag and the process group still apply and P17's allowlist entry
     stays honest.
   * check the assembled line against `maxCommandLineBatch`.
3. `launch_unix.go`: unchanged behaviour; only the signature follows if step 1
   changes it, and it routes through `procutil.Command` too.
4. `internal/provider/{codex,acpagent,httpagent}`: replace
   `procutil.Command(ctx, p.cfg.Bin, args...)` with `launch.Resolve` + `launch.Command`,
   reusing the `Resolved` the `Ready`/validation path already computes instead of
   resolving twice. On resolve failure the existing error path is kept.
5. Tests:
   * `launch_windows_test.go`: a table over probe 12 and 13's exact inputs —
     `a&calc`, `a|b`, `a>out.txt`, `a^b`, `a!PATH!`, `C:\Program Files (x86)\tool\x`,
     `two words`, `trailing\`, `a"b`, `%PATH%`, `a\nb`. Each case asserts either
     the refusal with its reason, or the exact `CmdLine` produced. The
     `(x86)` and `two words` cases assert acceptance — they are what the old
     allowlist got wrong, and they are the rows that prove this is a fix and not
     just a tightening.
   * an integration test, Windows-only, that writes a `.cmd` echoing `%*` into
     `t.TempDir()`, runs it through `launch.Command`, and asserts the batch
     received `a&calc` **literally** and that no extra process was created. This
     is the test that would have caught F30.
   * `launch_test.go`: the length ceiling per kind, and that `Command` refuses a
     line over the batch limit while allowing the same length for a native
     binary.
   * provider-level tests asserting each spawn goes through `launch.Command`
     (D24) — the existing fake-binary fixtures in `httpagent/provider_test.go`
     already resolve through `launch.Resolve`, so extend rather than invent.

**Verification (P19).**

```powershell
go test ./internal/provider/launch/ -v
go test ./internal/provider/... -run 'Spawn|Launch|Command'
go test -race ./internal/provider/...
make ci-windows
```

WSL lane: `go test ./internal/provider/...` on Linux, since `launch_unix.go` and
every provider file here compiles there. Mutations: restore the old allowlist and
the `(x86)` case fails; drop `/v:off` and the `a!PATH!` case fails; drop `/d` and
nothing fails — which is why F34's protection is asserted by a static check that
the built line contains `/d`, not by behaviour.

### P20 — The Windows page stops describing a guard that was never reached (D26)

1. `docs/ops-windows-install.md`, "Provider CLIs installed by npm": replace the
   claim that only the `.cmd` is launchable and that Windows requires
   `cmd.exe /c` (F33) with what was measured — the shim launches directly, cmd.exe
   is supplied implicitly, and `mcremote` therefore routes through it explicitly
   to control it with `/d /s /v:off`. Replace the rejected-character list with
   D23's: `"`, `%`, and line breaks, with the reason, and note that `&`, `|`,
   `(`, `)`, `^`, `!` and spaces are passed through literally. Keep the existing
   advice about pointing `bin` at the real executable, now as the remedy for a
   `%` in an argument.
2. "It runs without a window": add that the agent CLIs the daemon starts are
   windowless too from v0.19.0, and that v0.18.1 opened one per session which
   closing would kill.
3. Same list: a stopped session is now asked to drain first even when the daemon
   has no console, and how (borrowing the child's console). State the previous
   behaviour in one clause so an operator reading release notes can tell what
   changed.
4. A one-line security note that this closes a command-injection path for
   npm-shim providers (BatBadBut class, CVE-2024-24576), with the pointer to
   MADR 0159 F30.

**Verification (P20):** `markdownlint-cli2` clean; every statement checked
against the shipped code rather than against this plan, and the npm section
re-read against probe 12's output specifically.

### Acceptance criteria (additions)

| # | Criterion | MADR |
| --- | --- | --- |
| A15 | A child built by `procutil.Command` from a console-less parent reports `GetConsoleWindow() == 0`; the test fails pre-fix | D17, D20 |
| A16 | With a console, `creationFlags` adds nothing beyond `CREATE_NEW_PROCESS_GROUP` | D17 |
| A17 | No non-test file outside `internal/procutil` calls `exec.Command*`, except allowlisted paths that each carry a reason | D19 |
| A18 | A console-less parent stops a child politely: `TerminateProcessGroup` returns `true`, the child drains, and the parent survives | D22 |
| A19 | Two concurrent polite stops both succeed under `-race` | D22 |
| A20 | `a&calc` reaches a batch shim as the literal string, and `%` is refused with a reason | D23 |
| A21 | `C:\Program Files (x86)\tool\x` and `two words` are accepted | D23, F31 |
| A22 | The built batch line contains `/d /s /v:off /c`, asserted statically | D23, F34 |
| A23 | Each of the three providers spawns through `launch.Command` | D24 |
| A24 | A batch line over 8191 characters is refused; a native one of the same length is not | D25 |
| A25 | Owner, on v0.19.0: an agent session opens no window and survives; A11's logon half passes | D17, D21 |

**A16 and A22 are the two most likely to be dropped under pressure**, and for
the same reason: each asserts something that is *not* there. A16 guards the
interactive path from silently acquiring F24; A22 guards `/d`, whose absence
changes no observable behaviour on a host with no AutoRun — which is every
developer host until it is the one that matters.

### Rollout (v0.19.0)

1. P16 → P20, one commit each, Stability rule between them.
2. `make ci-windows-smoke` before the tag, per 0145.
3. Tag `v0.19.0` after a green CI run, on explicit instruction.
4. Owner: `mcremote update -y`; start an agent session — **no window, and it
   survives**; stop the session and confirm it is not hard-killed; then log off
   and on and start another, closing **A11's logon half** and **A25**.
5. Record results here, including anything the plan predicted incorrectly.

Rollback: no task definition and no installer behaviour changes, so `update`'s
binary rollback is the whole rollback. A downgrade to v0.18.x needs no
`setup-service --force`, because the task's arguments are untouched.

### Deferred (additions)

* **Resolving an npm shim to its real target.** It is the only way to make `%`
  representable and to leave cmd.exe out of the tree entirely: read the shim,
  find the `node` invocation and the script path, and spawn those. It is a
  parsing job against a format npm can change, so it needs its own decision and
  its own live-tagged test. Until then a `%` in an argument is refused with
  advice to point `bin` at the real executable.
* **Draining on the provider side.** D22 delivers the event; whether each CLI
  drains on it is the CLI's behaviour, and F35 says a Go one must listen on
  SIGINT. Measuring what grok, codex, opencode and kilo actually do with
  CTRL_BREAK belongs with the live-tagged provider suites.
* **P12 (`--env` through a private file)** follows in a later release,
  unchanged.
* **Moving `DetachConsole` into `procutil`.** One package owning console state
  would be tidier than P18's `NoteConsoleDetached` seam, but it moves a file
  0159 P13 just placed and re-opens the import-direction question for no
  behavioural gain.

### P16–P20: v0.19.0 code complete (2026-09-19)

All five phases ran, in order, one commit each. `make ci-windows` passed after
every phase; `make pre-add-check` passed on every Go file staged.

| Phase | Commit | What landed |
| --- | --- | --- |
| P16 | `c222966` | `procutil.Command`, `hasConsole`/`consoleState`/`creationFlags`, the three-deep window test |
| P17 | `99c69eb` | 25 spawn calls across 15 files moved to the constructor; the static ban |
| P18 | `246ac79` | `signalBreak` — attach, signal, detach — and `NoteConsoleDetached` |
| P19 | `5500d62` | The batch quoting guard, `launch.Command` wired into all three providers, split ceilings |
| P20 | `db92d59` | `docs/ops-windows-install.md` corrected |

**Mutation results.** Every guard was checked against a broken implementation,
because a test that passes against the bug is worth nothing.

| Mutation | Caught by | Evidence |
| --- | --- | --- |
| Drop `CREATE_NO_WINDOW` (pre-fix) | A15, A16 | `child console_hwnd = 0x4a091a` — the owner's symptom, reproduced |
| `hasConsole` → `GetConsoleWindow() == 0` | the new pseudoconsole test | `hasConsole() = false, but CONOUT$ open = true` |
| `CREATE_NO_WINDOW` unconditional | A16 | `creationFlags(true) = 0x8000200` |
| Reintroduce a bare `exec.Command` | A17 | named the file and line |
| A stale allowlist entry | A17's anti-rot check | named the entry |
| Pre-fix `signalBreak` (signal directly) | A18, A19 | `POLITE=false`, child killed instead of draining |
| Forget `defer FreeConsole` | A18 | `hasConsole=true` afterwards |
| Remove `consoleMu` | A19 | **failed 5 of 6 runs**, and 2 of 2 under `-race` |
| Drop per-argument quoting | A20 | `the shim received ARGS=[a], want ARGS=["a&calc"]` |
| Drop `/d` | A22 only | behaviour tests still passed |
| Drop `/v:off` | A22 only | behaviour tests still passed |

**Live evidence from the helper roles**, run directly rather than inferred from a
green suite: `PROCUTIL_POLITE=true exit=7` (the child drained on the signal
rather than being killed), `PROCUTIL_PARENT_ALIVE hasConsole=false` (the borrowed
console was given back and the borrower survived), and both children draining in
the concurrent case.

**Other gates.** `go test -race` clean on every touched package, `-count 2` on
`procutil`. The WSL Linux lane built everything and passed
`./internal/provider/...` plus `./internal/procutil/` (13 packages). `linux` and
`darwin` cross-builds clean.

### What the plan predicted incorrectly

1. **The mutation P16 relied on would not have been caught.** The plan said
   replacing `hasConsole()` with `GetConsoleWindow() == 0` "fails the
   with-console row". It does not: `creationFlags` takes a `bool`, so the table
   test never exercises the detection at all. The gap was found by trying the
   mutation rather than by reasoning about it. Closed by a new test that uses
   `CONOUT$` as an independent oracle — on this host it reports
   `attached=true console_hwnd=0x0`, so the discriminating case is live and the
   mutation now fails. **A plan that names a mutation should say which assertion
   catches it, and that claim needs checking like any other.**
2. **F28's count was low.** It said 16 non-test `exec.Command*` sites; the
   migration touched **25 calls in 15 files**, and removed **11** now-redundant
   `SetProcessGroup` calls. The original grep counted matching *lines*, including
   comments and packages outside the daemon.
3. **The console cache is a mutex and a `*bool`, not a `sync.Once`.** P18 needs
   to overwrite the cached answer from `NoteConsoleDetached`, which `sync.Once`
   cannot express. Deliberate deviation from P16 step 3.
4. **The mutex is better tested than predicted.** The plan expected the
   concurrency test to "fail or flake visibly" without it; it fails 5 of 6 runs,
   so `consoleMu` is genuinely test-enforced rather than defensive.
5. **`/v:off` is no more behaviour-testable than `/d`.** The plan singled out
   `/d` as invisible to behaviour tests. Delayed expansion is off by default, so
   `a!PATH!` is inert with or without `/v:off` too — both flags are guarded only
   by A22's static assertion, which now covers all four.
6. **The trailing backslash is refused.** P19 left this to measurement. `cmd.exe`
   does not treat `\` as an escape while `CommandLineToArgvW` does, so no single
   spelling satisfies both the shim and whatever the shim execs; refusing is the
   only answer that is correct for both.
7. **`os.StartProcess` was added to the ban**, which the plan did not mention.
   Banning only `exec.Command*` would leave an equivalent bypass one layer down.
8. **P19 had to delete two allowlist entries**, which the plan anticipated as a
   possibility and which the anti-rot check turned into a failing test rather
   than a silent staleness. The allowlist is down to one entry,
   `codesign_darwin.go`.
9. **`detach_other.go` needed no change.** P18 step 2 called for a matching
   no-op; only the Windows `DetachConsole` calls `NoteConsoleDetached`, so there
   is no cross-platform signature to keep parity with.
10. **One test contract was deliberately replaced, not extended.**
    `TestCommandRejectsCmdMetacharacters` asserted that every cmd.exe
    metacharacter is refused; D23 makes most of them legal-and-quoted, so it is
    now `TestCommandRefusesOnlyWhatQuotingCannotFix`. The reason is recorded in
    the test itself, and the characters it stopped refusing are proven inert
    against a real shim by `TestBatchArgumentsReachTheShimLiterally`. This is the
    one place in the plan where an assertion got weaker, and it is compensated
    rather than dropped.
11. **`procutil_other.go`'s `newCommand` cannot be compile-checked here.**
    `GOOS=js` fails in `internal/fsutil` and `internal/appdirs`, which have no
    implementation for that platform. Confirmed pre-existing by building the same
    target at `HEAD`. The residual-platform constructor mirrors the Unix one and
    is unverified by any build.
12. **A process mistake worth recording.** Mid-mutation-test, the working copy of
    `procutil_windows.go` was restored with `git checkout --` while P16 was still
    uncommitted, which reverted the implementation to `HEAD` and turned the next
    two mutations into build failures instead of test failures. It was recovered
    from a copy taken beforehand. **While a phase is uncommitted, a mutation test
    must restore from a file copy, never from git.**

### Not yet done

* ~~v0.19.0 is an owner action~~ — **done 2026-09-20**, see the rollout record
  above. A11's logon half and A25 are both closed.
* ~~`make ci-windows-smoke`~~ — **run before the tag**, passed.
* P12 (`--env` through a private file) follows in a later release, unchanged.
* Codex sessions on this host stay broken until the owner restores the ACL on
  `~/.codex/config.toml`. Nothing in this plan is waiting on it.
