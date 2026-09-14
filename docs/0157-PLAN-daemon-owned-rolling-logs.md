---
status: proposed
date: 2026-09-13
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0157 — Daemon-owned rolling logs

Implements [0157-MADR-daemon-owned-rolling-logs.md](0157-MADR-daemon-owned-rolling-logs.md)
decisions D1–D14, closing findings F1–F15.

## Goal

Every daemon writes its own bounded, rotating log file on Linux, macOS, and
Windows, without depending on systemd, launchd, or Task Scheduler; and the
resolved path, the docs, and the Windows acceptance script agree on one value.

Finish line (observable states):

* `mcremote paths --json` and `mcrelay paths --json` both emit a `log_dir` that
  equals the MADR D3 table on the host's OS, with no doubled product leaf;
* `mcremote serve` and `mcrelay serve` create `<log_dir>/<product>.log`, append
  across restarts, rotate at the configured size, retain at most
  `max_backups`, drop backups older than `max_age_days`, and gzip them when
  `compress` is true;
* every record also reaches `stderr`, so journald/launchd/interactive serve are
  unchanged;
* a rotation failure keeps writing and emits one warning; no line is lost;
* `log.file: "off"` produces no file;
* `mcremote setup-service` on Windows prints the real log path and a
  `Get-Content -Wait` command;
* `make test`, `make race`, `make lint`, `make tidy`, `make preflight`,
  `make ci-windows`, and `scripts/acceptance-windows.ps1` are green.

## Scope

### In scope (the only files any phase may touch)

* `internal/appdirs/paths.go` — `LogDir` via `joinProduct` (D3)
* `internal/appdirs/roots_unix.go` — Linux `Logs` base (D3)
* `internal/appdirs/paths_test.go`, `internal/appdirs/systempaths_test.go`,
  `internal/appdirs/roots_windows_test.go` — path tests (D3)
* `internal/logging/slog.go` — `Options`, `Setup`, `Open` (D2, D13)
* `internal/logging/rotate.go`, `internal/logging/rotate_test.go` — rotator (D6, D7, D9)
* `internal/logging/slog_test.go` — `Open` tests (D13)
* `internal/config/config.go` — `LogConfig`, defaults, `Validate`, `LogFilePath` (D4, D5, D8)
* `internal/config/load.go` — env bindings and flag map (D4)
* `internal/config/config_test.go` — config tests (D4, D5)
* `internal/relay/fileconfig.go` — `LogConfig`, defaults, `Validate`, `LogFilePath`, env/flag map (D4, D5, D8, D11)
* `internal/relay/fileconfig_test.go` — mirror tests (D4, D5)
* `internal/relay/cli.go` — flags, `serve` wiring, `paths` `log_dir` (D4, D8, D11, D13)
* `internal/relay/cli_test.go` — paths/wiring tests (D11)
* `internal/cli/root.go` — persistent log flags (D4)
* `internal/cli/serve.go` — `logging.Open` wiring (D1, D2, D8, D13)
* `internal/cli/doctor.go` — log-file report (D14)
* `internal/cli/serve_test.go`, `internal/cli/doctor_test.go`,
  `internal/cli/root_test.go` — CLI tests (D4, D13, D14)
* `internal/cli/service/setup.go` — `Result.LogDir` all OSes (D10)
* `internal/cli/service/result_print.go` — `windows-task` case (D10)
* `internal/cli/service/setup_test.go` — service tests (D10)
* `internal/cli/service/result_print_test.go` — new; `windows-task` output (D10)
* `internal/cli/service/defaults_mcremote.yaml`, `internal/cli/service/defaults_mcrelay.yaml` — embedded defaults (D12)
* `internal/cli/examples.go` — examples (D12)
* `docs/config.md`, `docs/config-mcrelay.md`, `docs/ops-windows-install.md` — docs (D12)
* `scripts/acceptance-windows.ps1` — Windows acceptance (D12)

### Out of scope

* `gopkg.in/natefinch/lumberjack.v2`, `github.com/DeRuina/timberjack`, or any
  new module (MADR D9) — `go.mod`/`go.sum` must end byte-identical.
* Changing `daemon.Run` to create the log file (the sink owns it, D1); the
  `setup-service` task/plist/unit argv and the schtasks XML are untouched.
* Removing journald/launchd capture (MADR D2 keeps it).
* SIGHUP rotation, instance-keyed filenames, zstd, and content scanning in
  `doctor` — see Deferred.
* Any `apps/mobile` Dart change.

## Stability rule

Every phase ends with, from the repository root:

```bash
gofmt -l cmd internal
go vet ./...
go test ./...
go test -race ./...
```

All four must be clean (`gofmt -l` prints nothing; tests pass). Stage with the
pre-add gate active (AGENTS.md: `gofmt`, `golint`, `govulncheck` clean), then
**one commit** per phase (`git commit --no-edit`; never `-m`). **No `git push`
and no tags** are authorized by this plan; each is a separate explicit ask.

## Cross-cutting contracts

**C1 — No new module dependency.** `git diff --exit-code go.mod go.sum` is empty
at every phase end. Violating it looks like a rotation library import.

**C2 — Three surfaces or none.** Every new setting is settable through
`log.*` config, its `MCREMOTE_`/`MCRELAY_` env var, and its flag, in the same
phase that introduces it. Violating it looks like a config key with no flag.

**C3 — Rotation never drops or reorders a line.** A failed rotate/prune/compress
leaves the sink writable and returns no error to the writer; records are written
under one mutex. Violating it looks like `os.Rename` erroring and the next log
line disappearing.

**C4 — The sink is additive.** `stderr` receives every record the file does;
`Options.Out` (tests) still wins for injection. Violating it looks like a
daemon that is silent on a terminal.

**C5 — Tests are amended, never weakened.** `TestSystemRootsDispatches` is
changed to assert the *new* Linux `Logs` value, not deleted; no `t.Skip` is
added to a passing test.

**C6 — One path, three references.** `paths` output, docs, and
`acceptance-windows.ps1` name the same `LogDir` per OS. This is the contract
most likely to drift under time pressure, because the Windows assertion lives in
a script only a Windows host runs and will not fail on CI.

**C7 — Platform claims are proven or marked.** The Windows rename-while-tailed
behaviour (MADR open question 1) is either pinned with a host probe or shipped
behind D7's documented fallback; it is never asserted from assumption.

## Dependency and delivery order

`P1 → P2 → P3 → P4 → P5 → P6 → P7`. P1 is independent of the logging work and
may land first alone. P2 (rotator) precedes P3 (config) because P3's unit tests
construct rotator configs; P3 precedes P4 (wiring); P5 (reporting) needs P4's
path helper; P6/P7 need every prior phase. No phase may be reordered to "unblock"
a later one.

## Implementation Steps

### P1 — Unify the resolved log directory (D3; closes F4, F5, F15)

1. `internal/appdirs/paths.go`: replace the raw join at line 70 with
   `p.LogDir = roots.joinProduct(roots.Logs, product.Name)` and delete the now
   unused `label` block (lines 64–69, 71). Keep the `roots.Logs != ""` guard.
2. `internal/appdirs/roots_unix.go`: replace the darwin-only assignment at
   lines 52–54 with a switch that always sets a base:
   `darwin` → `filepath.Join(home, "Library", "Logs")`;
   otherwise (linux) → `xdgOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))`.
   Update the comment to cite MADR 0157 D3 (it no longer needs MADR 0116 F4's
   "only darwin" rule).
3. `internal/appdirs/systempaths_test.go`: change `TestSystemRootsDispatches`
   so both darwin and linux assert a non-empty `r.Logs` (`~/Library/Logs` and
   `$XDG_STATE_HOME` respectively); keep the `ProductScoped` assertion.
4. `internal/appdirs/roots_windows_test.go`: extend
   `TestResolveWindowsDoesNotDoubleJoin` to assert
   `p.LogDir == filepath.Join(r.Logs)` and that it contains the product leaf
   exactly once.
5. `internal/appdirs/paths_test.go`: add
   `TestResolveLogDirScopedRootUnchanged` (a `ProductScoped` roots value yields
   `LogDir == r.Logs`) and keep
   `TestResolveLeavesLogDirEmptyWithoutLogsRoot` unchanged.

**Verification:**

```bash
go test ./internal/appdirs/... -count=1
go run ./cmd/mcremote paths --json   # Windows: "…\\mcremote\\Logs" once; Linux: "…/.local/state/mcremote"
```

### P2 — The rotator (D6, D7, D9; closes F12)

New `internal/logging/rotate.go` (package `logging`), no imports outside the
standard library and `internal/appdirs`:

```go
type RotateConfig struct {
    Filename   string        // required, absolute
    MaxSize    int64         // bytes; 0 = unbounded
    MaxBackups int           // 0 = unbounded
    MaxAge     time.Duration // 0 = unbounded
    Compress   bool
    Now        func() time.Time // nil = time.Now (test seam)
    OnError    func(error)      // nil = os.Stderr (test seam)
}

type RotatingFile struct { /* mutex, *os.File, size, cfg */ }

func OpenRotatingFile(cfg RotateConfig) (*RotatingFile, error)
func (f *RotatingFile) Write(p []byte) (int, error)
func (f *RotatingFile) Close() error
```

Semantics (exactly):

1. `OpenRotatingFile` calls `appdirs.EnsurePrivateDir(filepath.Dir(Filename))`,
   opens `O_CREATE|O_APPEND|O_WRONLY` mode `0600`, records `Stat().Size()`, and
   rotates once if `MaxSize > 0 && size >= MaxSize`.
2. `Write` locks; if `MaxSize > 0 && f.size > 0 && f.size+len(p) > MaxSize`,
   calls `rotate()`. If `rotate` returns an error, `Write` calls `OnError`
   (once per distinct error), reopens the current filename for append if it is
   closed, and still writes `p`. `Write` never returns a rotation error.
3. `rotate` closes the file, renames it to `<base>-<UTC 2006-01-02T15-04-05.000><ext>`
   (UTC from `Now`), opens a fresh `O_CREATE|O_TRUNC|O_WRONLY` `0600` active
   file, sets `size = 0`, then prunes and compresses. A failed rename reopens the
   original for append and returns the rename error to `Write`'s best-effort
   handler.
4. `prune`: `os.ReadDir(dir)`, select names with prefix `<base>-` and suffix
   `<ext>` or `<ext>.gz` whose middle parses as the timestamp; sort newest first;
   remove beyond `MaxBackups`; remove older than `MaxAge`; if `Compress`, gzip
   each uncompressed backup to `<name>.gz` and remove the original.
5. Package vars `nowFn = time.Now` and `renameFile = os.Rename` exist only as
   test seams; production leaves them at their defaults.

`internal/logging/rotate_test.go` (all deterministic via `Now`/`renameFile`):

```text
TestRotateAtSize                     tiny MaxSize; N writes → active + ≥1 backup, name pattern matches
TestRotateOnOpenOverSize             pre-write an oversized active file; Open → it is rotated before the first write
TestAppendAcrossReopen               write, Close, Open, write → both payloads present in the active file
TestMaxBackupsPruned                 8 rotations, MaxBackups=2 → exactly 2 backups remain
TestMaxAgePruned                     advance Now past MaxAge → old backups removed
TestCompressProducesGz               Compress=true → backup is .log.gz and gunzips to the original bytes
TestRotationFailureKeepsWriting       renameFile errors → later writes still land; OnError called; Write err == nil
TestSingleRecordOverCap              one write > MaxSize → payload fully present, not dropped
TestConcurrentWrites                 16 goroutines × 100 lines → line count exact, no partial line
TestNoRotationWhenUnbounded          MaxSize=0, MaxBackups=0, MaxAge=0 → one file, no backup
```

**Verification:**

```bash
go test -race ./internal/logging/ -run 'Rotate|Concurrent|Unbounded' -count=1 -v
```

### P3 — Config, env, and flag surface for both products (D4, D5; closes F14)

mcremote (`internal/config/config.go`, `internal/config/load.go`,
`internal/cli/root.go`):

1. Extend `LogConfig` (config.go:416–420):
   `File string mapstructure:"file"`, `MaxSizeMB int mapstructure:"max_size_mb"`,
   `MaxBackups int mapstructure:"max_backups"`,
   `MaxAgeDays int mapstructure:"max_age_days"`,
   `Compress bool mapstructure:"compress"`.
2. `Defaults()` (config.go:797–800): `File: ""`, `MaxSizeMB: 10`,
   `MaxBackups: 5`, `MaxAgeDays: 28`, `Compress: true`.
3. `setDefaults` (load.go:280–281): five `v.SetDefault("log.*", …)` lines.
4. `BindEnv` (load.go:45–46): `MCREMOTE_LOG_FILE`,
   `MCREMOTE_LOG_MAX_SIZE_MB`, `MCREMOTE_LOG_MAX_BACKUPS`,
   `MCREMOTE_LOG_MAX_AGE_DAYS`, `MCREMOTE_LOG_COMPRESS`.
5. `bindFlags` (load.go:404–405): `"log-file"`, `"log-max-size-mb"`,
   `"log-max-backups"`, `"log-max-age-days"`, `"log-compress"` → the five keys.
6. `internal/cli/root.go` var block (20–23) and persistent flags (88–89): add
   the five flags (`StringVar`, `IntVar`, `IntVar`, `IntVar`, `BoolVar`).
7. `Validate` (config.go:1119–1128): reject a `log.file` containing `\n`, `\r`,
   or `\x00`; reject `MaxSizeMB < 0`, `MaxBackups < 0`, `MaxAgeDays < 0`
   (`0` is allowed and means unbounded).
8. Add to config.go:

```go
// LogFilePath resolves log.file: "off" disables the file sink, "" selects the
// platform default under Paths.LogDir, anything else is used verbatim.
func (c Config) LogFilePath() string
// LogFileRequired is true only when the operator set an explicit path.
func (c Config) LogFileRequired() bool
```

mcrelay (`internal/relay/fileconfig.go`, `internal/relay/cli.go`) mirrors all
eight items: `LogConfig` (fileconfig.go:143–147), `DefaultsFile`,
`setFileDefaults` (480–481), `BindEnv` (230–231), `bindRelayFlags` (519–520),
new local flags in `newRootCmd` (54–57, 90–91), `Validate` (560–569), and
`LogFilePath`/`LogFileRequired` with `"mcrelay.log"`.

`internal/config/config_test.go` and `internal/relay/fileconfig_test.go`: add
cases that each of the five keys resolves from YAML, env, and flags; that the
defaults are `10/5/28/true` and `file: ""`; and that the three invalid values
above are rejected with a message naming the key. The env cases use `t.Setenv`.

**Verification:**

```bash
go test ./internal/config/ ./internal/relay/ -run 'Log|Validate|Default|Flag' -count=1
go run ./cmd/mcremote serve --help | Select-String 'log-file|log-max-size-mb'
go run ./cmd/mcrelay  serve --help | Select-String 'log-file|log-max-size-mb'
```

### P4 — Wire the sink into both daemons (D1, D2, D8, D13; closes F1, F2, F3, F6, F10)

1. `internal/logging/slog.go`: keep `Setup` byte-for-byte behaviourally (it
   builds the handler over `opts.Out` or `os.Stderr`). Extend `Options` with
   `File string`, `RequireFile bool`, `MaxSizeBytes int64`, `MaxBackups int`,
   `MaxAge time.Duration`, `Compress bool`, `OnError func(error)`. Add:

```go
// Open builds a logger and, when opts.File is set, a rotating file the caller
// must Close. On failure: a required file is a startup error; a default file
// warns once on stderr and falls back to stderr-only logging (D8).
func Open(opts Options) (*slog.Logger, io.Closer, error)
```

   `Open` returns `Setup(opts)` with a no-op `io.Closer` when `File == ""`.
   Otherwise it calls `OpenRotatingFile`, sets
   `opts.Out = io.MultiWriter(rf, os.Stderr)` (D2), and returns `Setup(opts)`
   with `rf` as the closer. `opts.Out != nil` takes precedence over `File`
   (tests inject a buffer).

2. `internal/cli/serve.go` (80–83): replace `logging.Setup(...)` with
   `logger, closer, err := logging.Open(logging.Options{…})`; return `err`; and
   `defer closer.Close()`. Populate from `cfg.Log` and
   `filepath.Dir(cfg.LogFilePath())`/`cfg.LogFileRequired()`. Add a small
   unexported helper `logSinkOptions(cfg config.Config, product string)` so the
   mapping is unit-testable.

3. `internal/relay/cli.go` (257–260): same replacement using `fc.Paths.LogDir`
   and `"mcrelay"`.

4. No change to `daemon.Run`; it keeps converging `DataDir`/`ConfigDir` only.

`internal/logging/slog_test.go`: add `TestOpenNoFile` (no `File` → `NopCloser`,
stderr path), `TestOpenWritesFileAndStderr` (temp `File` + buffer `Out`; assert
both receive the line), `TestOpenOffIsHandledByCaller` (empty `File`),
`TestOpenRequireFileFails` (unwritable path + `RequireFile` → error),
`TestOpenDefaultPathWarnsAndFallsBack` (unwritable path, `RequireFile=false` →
non-nil logger, nil error, no panic). Add
`internal/cli/serve_test.go` `TestLogSinkOptionsDefaultOffExplicit` covering
`""`→default path, `"off"`→empty sink, explicit→verbatim, and `RequireFile`
true only for the explicit case.

**Verification:**

```bash
go test -race ./internal/logging/ ./internal/cli/ -run 'Open|LogSink|Serve' -count=1 -v
```

Then, once by hand, confirm the live behaviour the unit tests cannot: run
`mcremote serve --tls=false --log-file "$TMP/m.log"` in the foreground, see the
`starting mcremote` line on the terminal, confirm `$TMP/m.log` contains it, and
Ctrl-C — the file closes. Repeat with `--log-file off` and confirm no file is
created.

### P5 — Service reporting and doctor (D10, D14; closes F7, F8, F9)

1. `internal/cli/service/setup.go`: in `Setup`, after `normalize`, resolve
   `p, err := resolveProductPaths(opts.Product, opts.DataDir)` and set
   `res.LogDir = p.LogDir` for every OS. In `setupLaunchdAgent` (431–435),
   replace the hardcoded path with `res.LogDir` (keep the `MkdirAll`).
2. `internal/cli/service/result_print.go`: add
   `case "windows-task":` beside `launchd-agent` in the display, summary, and
   status switches (44–56, 95–109, 118–135): print
   `Logs:    <res.LogDir>\<product>.log` and
   `Tail:    Get-Content -Wait "<file>"`, and
   `Status:  schtasks /query /tn <unit> /fo LIST /v`, `Stop: schtasks /end /tn <unit>`,
   `Remove: <product> setup-service --remove`.
3. `internal/relay/cli.go` `newPathsCmd` (126–150): add `"log_dir": cfg.Paths.LogDir`
   to the JSON map and `log_dir: <path>` to the text output, only when non-empty.
4. `internal/cli/doctor.go`: load config through the same
   `config.Load(config.LoadOptions{ConfigFile: cfgFile, Flags: cmd.Flags()})`
   call the root already supplies to `paths`, resolve
   `logPath := cfg.LogFilePath()`, and add
   `renderLogDoctor(w, logPath, probeLogFile(logPath))` after the credential
   section. `probeLogFile` returns `{present bool, size int64, writable bool}`
   by `os.Stat` plus an `O_WRONLY|O_APPEND` open attempt; it never writes.

`internal/cli/service/setup_test.go`: assert `res.LogDir` is non-empty on the
systemd, launchd, and schtasks (via `OverrideInstallOS`) paths.
`internal/cli/service/result_print_test.go` (create if absent): assert the
`windows-task` summary prints the log path and no `journalctl`.
`internal/relay/cli_test.go`: extend the paths JSON/text tests (72–150) for
`log_dir`. `internal/cli/doctor_test.go` (create if absent):
`TestRenderLogDoctor` covers present/absent/unwritable.

**Verification:**

```bash
go test ./internal/cli/service/ ./internal/cli/ ./internal/relay/ -run 'LogDir|PrintSetup|Doctor|Paths' -count=1 -v
go run ./cmd/mcremote doctor
go run ./cmd/mcrelay  paths --json | Select-String log_dir
```

### P6 — Docs, defaults, and examples (D12; closes F9, F15)

1. `docs/config.md`: add the five rows to the defaults table beside
   `log.level`/`log.format` (106–107); add the five `MCREMOTE_LOG_*` rows to the
   env table (409–410); add them to the flag reference wherever `--log-level`
   appears. State the D5 defaults and the `off` sentinel.
2. `docs/config-mcrelay.md`: same three edits beside lines 69–70, 145–146, and
   324–325.
3. `docs/ops-windows-install.md`: in the layout table (51–58) keep the `Logs`
   row, and specify the active file
   `%LocalAppData%\mcremote\Logs\mcremote.log`; add a short "Logs" subsection
   showing `Get-Content -Wait` and the rotation knobs.
4. `internal/cli/service/defaults_mcremote.yaml` (27–29) and
   `defaults_mcrelay.yaml` (45–47): add the five keys with D5 defaults and a
   trailing comment naming the env var and flag, matching the file's existing
   comment style.
5. `internal/cli/examples.go`: update the tail example at line 176 to
   `tail -f ~/Library/Logs/mcremote/mcremote.log`, and add one `--log-file`
   example next to the `--log-format` examples (14–16, 55–56).

**Verification:**

```bash
go test ./internal/cli/service/ -run 'Default' -count=1
Select-String -Path docs\config.md,docs\config-mcrelay.md -Pattern 'log.max_size_mb|log.file'
Select-String -Path internal\cli\service\defaults_*.yaml -Pattern 'max_size_mb|compress'
```

### P7 — Windows acceptance (D1, D12; closes F2, F4; pins MADR open question 1)

1. `scripts/acceptance-windows.ps1`: after the `paths --json (C1)` check
   (142–144), add:
   * an assertion that `$j.log_dir` equals `Join-Path $local 'mcremote\Logs'`
     (already present; it now passes after P1);
   * a functional check: create temp `$tmpLog` and `$tmpData`, pick a free high
     port, start
     `& $bin serve --tls=false --listen-host 127.0.0.1 --listen-port $port --data-dir $tmpData --log-file $tmpLog`
     as a background process, poll up to 15 s for `$tmpLog`, assert it exists
     and contains `starting mcremote`, then stop the process (and remove the
     temp dirs) in a `finally`;
   * the `off` check: a short `serve --log-file off …` run creates no file;
   * the rename-while-tailed probe (C7): while `Get-Content -Wait $tmpLog` is
     attached, force a rotation by restarting with `--log-max-size-mb 1` and
     writing enough output, and record whether rotation succeeded. If it fails,
     assert the daemon kept logging and emitted one warning (D7), and add a
     one-line note to `docs/ops-windows-install.md`; if it succeeds, state that
     in the script comment.
2. Keep the script Windows-only and `exit 0` off-Windows (its existing guard
   25–34).

**Verification (Windows host):**

```powershell
make ci-windows
scripts/acceptance-windows.ps1
```

## Verification (whole plan)

```bash
make test
make race
make lint
make tidy
make preflight
git diff --exit-code go.mod go.sum
```

On the Windows host, additionally:

```powershell
make ci-windows
make ci-windows-smoke
scripts/acceptance-windows.ps1
```

**Not verifiable on this host:** the Windows rename-while-tailed probe (P7) and
the Windows Task Scheduler registration require the owner's Windows laptop; the
macOS launchd regression check (`launchctl print`, `mcremote.err.log`) requires a
Mac. Both are stated here rather than skipped silently; P7 records their result.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `mcremote paths --json` and `mcrelay paths --json` emit the D3 `log_dir` on the host OS, product leaf once | D3, D11 |
| A2 | `mcremote serve` creates `<log_dir>/mcremote.log`, appends across a restart, and closes on SIGTERM | D1, D13 |
| A3 | The same run's records also reach `stderr` | D2 |
| A4 | With `max_size_mb=1`, `max_backups=2`, `compress=true`: rotation happens, exactly ≤2 backups remain, backups end `.log.gz` | D5, D6 |
| A5 | A forced rotate failure keeps writing, warns once, and returns no write error (unit test `TestRotationFailureKeepsWriting`) | D7 |
| A6 | `log.file: "off"` (and `--log-file off`) creates no file | D5 |
| A7 | An explicitly set, unopenable `log.file` fails startup; the unopenable default warns and continues | D8 |
| A8 | Windows `setup-service` prints the log path and `Get-Content -Wait`, and no `journalctl` line | D10 |
| A9 | `mcremote doctor` names the log path, size, and writability | D14 |
| A10 | The five keys resolve from config, env, and flag for both products; invalid values are rejected | D4 |
| A11 | `go.mod`/`go.sum` are unchanged | D9 |
| A12 | `scripts/acceptance-windows.ps1` passes on the Windows host, including `paths --json (C1)` | D12 |

Most likely to be quietly dropped: **A12**, because it is the only criterion
that cannot fail on CI and needs a physical Windows host. It would be tempting
to declare the plan done on a green `make preflight`; the plan is not done until
the Windows script (or an explicit statement that the host run is scheduled)
records its result.

## Rollout and Rollback

What a user observes: after the first daemon start, a new file appears at the
documented `log_dir`; it grows, rotates, and gzips; `stderr`/journald/launchd
behaviour is unchanged; `setup-service` and `doctor` name the file. `log.file:
"off"` restores a file-free daemon in one setting.

Rollback is per-phase `git revert` of the phase commit; no migration runs and no
state format changes. Reverting P4 alone stops file creation everywhere while
leaving the path fix (P1) and reporting (P5) harmless.

## Deferred (named, so they are not mistaken for oversights)

* **SIGHUP / manual rotation** (MADR open question 3) — waits for a second
  record; rotation-on-size already bounds the file, and Windows has no SIGHUP.
* **Instance-keyed filenames** (D6) — per-product matches existing macOS
  behaviour; a second `--data-dir` instance shares the file. Belongs to a
  follow-up if multi-instance hosts become supported.
* **zstd compression** — gzip is stdlib and sufficient; zstd would add a module
  (C1) for marginal gain.
* **`doctor` content scanning / error summarising** (D14 limits it to path,
  size, writability) — a different feature with its own privacy review.
* **Windows Event Log mirroring** — the file is the contract; an Event Log sink
  is a separate integration.
