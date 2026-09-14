---
status: proposed
date: 2026-09-13
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0157 — Daemon-owned rolling logs

Implements [0157-MADR-daemon-owned-rolling-logs.md](0157-MADR-daemon-owned-rolling-logs.md)
decisions D1–D14, closing findings F1–F18.

All file:line anchors below were re-verified against the tree on 2026-09-13
(commit `e01f728`). The Windows rotation semantics this plan relies on are
measured, not assumed: host probe 2026-09-13, go1.26.6 windows/amd64, 3/3
identical runs (MADR F16).

## Goal

Every daemon writes its own bounded, rotating log file on Linux, macOS, and
Windows, without depending on systemd, launchd, or Task Scheduler; and the
resolved path, the docs, and the Windows acceptance script agree on one value.

Finish line (observable states):

* `mcremote paths --json` and `mcrelay paths --json` both emit a `log_dir` that
  equals the MADR D3 table on the host's OS, with no doubled product leaf; on
  Linux `.log_dir == .state_dir` (F17, pinned by a test, not by hope);
* `mcremote serve` and `mcrelay serve` create `<log_dir>/<product>.log`, append
  across restarts, rotate at the configured size, retain at most
  `max_backups`, drop backups older than `max_age_days`, and gzip them when
  `compress` is true;
* a backup-name collision yields a `-1`/`-2`/… suffix; an existing backup is
  never silently overwritten (F16, C8);
* every record also reaches the non-file sink (`stderr` in production), so
  journald/launchd/interactive serve are unchanged — even when the file write
  fails or `stderr` has no reader (C4);
* a rotation failure keeps writing and emits one warning per distinct error;
  no line is lost (C3);
* `log.file: "off"` produces no file;
* `mcremote setup-service` on Windows prints the real log path, a
  `Get-Content -Wait` command, and the re-attach caveat (F16);
* `mcremote doctor` names the resolved log path, its size, and writability;
* P7's rotation run records the observed active-plus-backups size (MADR OQ2),
  turning the 60 MB bound into a measured number;
* `make test`, `make race`, `make lint`, `make tidy`, `make preflight`,
  `make ci-windows`, and `scripts/acceptance-windows.ps1` are green.

## Scope

### In scope (the only files any phase may touch)

* `internal/appdirs/paths.go` — `LogDir` via `joinProduct` (D3)
* `internal/appdirs/roots_unix.go` — Linux `Logs` base (D3)
* `internal/appdirs/paths_test.go`, `internal/appdirs/systempaths_test.go`,
  `internal/appdirs/roots_windows_test.go` — path tests (D3, F17)
* `internal/logging/slog.go` — `Options`, `Setup`, `Open`, tee writer (D2, D13)
* `internal/logging/rotate.go`, `internal/logging/rotate_test.go` — rotator (D6, D7, D9)
* `internal/logging/slog_test.go` — `Open` tests (D13)
* `internal/config/config.go` — `LogConfig`, defaults, `Validate`, `LogFilePath`,
  `LogFileRequired` (D4, D5, D8)
* `internal/config/load.go` — env bindings and flag map (D4)
* `internal/config/config_test.go` — config tests (D4, D5, C9)
* `internal/relay/fileconfig.go` — `LogConfig`, defaults, `Validate`,
  `LogFilePath`, `LogFileRequired`, env/flag map (D4, D5, D8, D11)
* `internal/relay/fileconfig_test.go` — mirror tests (D4, D5, C9)
* `internal/relay/cli.go` — flags, `serve` wiring, `paths` `log_dir` (D4, D8, D11, D13)
* `internal/relay/cli_test.go` — paths/wiring tests (D11)
* `internal/cli/root.go` — persistent log flags (D4)
* `internal/cli/serve.go` — `logging.Open` wiring, `logSinkOptions` (D1, D2, D8, D13)
* `internal/cli/doctor.go` — log-file report (D14)
* `internal/cli/serve_test.go`, `internal/cli/doctor_test.go`,
  `internal/cli/root_test.go` — CLI tests (D4, D13, D14)
* `internal/cli/service/setup.go` — `Result.LogDir` all OSes (D10)
* `internal/cli/service/result_print.go` — `windows-task` case (D10)
* `internal/cli/service/setup_test.go` — service tests (D10)
* `internal/cli/service/result_print_test.go` — new; `windows-task` output (D10)
* `internal/cli/service/defaults_mcremote.yaml`,
  `internal/cli/service/defaults_mcrelay.yaml` — embedded defaults (D12)
* `internal/cli/examples.go` — examples (D12)
* `docs/config.md`, `docs/config-mcrelay.md`, `docs/ops-windows-install.md` — docs (D12)
* `scripts/acceptance-windows.ps1` — Windows acceptance (D12)

`internal/cli/paths.go` (mcremote) needs **no change**: it already emits
`log_dir` in JSON (`omitempty`, `:68`) and text (conditional, `:91-93`).

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

**C4 — The sink is additive and order-independent.** The non-file sink receives
every record even when the file write fails, and the file receives every record
even when the non-file sink fails (a detached Windows daemon's `stderr` has no
reader). `Options.Out` (tests) replaces the non-file sink only. Violating it
looks like `io.MultiWriter`'s abort-on-first-error semantics being used — it
loses the second copy whenever the first writer fails.

**C5 — Tests are amended, never weakened.** `TestSystemRootsDispatches` is
changed to assert the *new* Linux `Logs` value, not deleted; no `t.Skip` is
added to a passing test.

**C6 — One path, three references.** `paths` output, docs, and
`acceptance-windows.ps1` name the same `LogDir` per OS. This is the contract
most likely to drift under time pressure, because the Windows assertion lives in
a script only a Windows host runs and will not fail on CI.

**C7 — Platform claims are proven or marked.** The Windows rename-while-tailed
behaviour was probed on the host 2026-09-13 (MADR F16, 3/3 runs): rename
succeeds, rename-over-existing replaces silently, open backups are removable.
P7 re-pins this on the acceptance host; D7's keep-appending fallback remains
the portable behaviour for a genuinely failed rename. No new platform claim may
be asserted from assumption.

**C8 — A backup is never silently overwritten.** Because Windows
`os.Rename` replaces an existing target (F16), rotation must probe for a free
backup name (`-1`, `-2`, … suffix before the extension) instead of renaming
blind. Violating it looks like a one-line `os.Rename` in `rotate()`.

**C9 — Flag zero-defaults never shadow config defaults.** The int/bool knobs
are registered with zero-value flag defaults and rely on viper's
`Changed`-gated `BindPFlag` (F18: `load.go:435`, `fileconfig.go:545`); a test
in each product asserts that a Load with the flags registered but untouched
yields `10/5/28/true`, not `0/0/0/false`. Violating it looks like
`v.SetDefault` being skipped "because the flag has a default".

## Dependency and delivery order

`P1 → P2 → P3 → P4 → P5 → P6 → P7`. P1 is independent of the logging work and
may land first alone. P2 (rotator) precedes P3 (config) because P3's unit tests
construct rotator configs; P3 precedes P4 (wiring); P5 (reporting) needs P4's
path helper; P6/P7 need every prior phase. No phase may be reordered to "unblock"
a later one.

## Implementation Steps

### P1 — Unify the resolved log directory (D3; closes F4, F5, F15, F17)

1. `internal/appdirs/paths.go`: inside the `if roots.Logs != ""` guard
   (lines 63–72), replace the raw join at line 70 with
   `p.LogDir = roots.joinProduct(roots.Logs, product.Name)` and delete the dead
   `label` block (lines 64–67, 68–69 comment, 71 `_ = label`). `joinProduct`
   (`roots.go:29-35`) returns the base unchanged when `ProductScoped` is true —
   that is the Windows double-leaf fix — and appends the product leaf otherwise.
2. `internal/appdirs/roots_unix.go`: replace the darwin-only assignment at
   lines 52–54 with a switch that always sets a base:
   `darwin` → `filepath.Join(home, "Library", "Logs")`;
   otherwise (linux) → `xdgOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))`.
   Replace the comment at 48–51: cite MADR 0157 D3 and state that on Linux this
   deliberately equals `StateHome` (F17).
3. `internal/appdirs/systempaths_test.go`: change `TestSystemRootsDispatches`
   (lines 33–42) so both darwin and linux assert a non-empty `r.Logs`
   (`~/Library/Logs` and `$XDG_STATE_HOME` respectively); keep the
   `ProductScoped` assertion at 30–32. Amend, do not delete (C5).
4. `internal/appdirs/roots_windows_test.go`: extend
   `TestResolveWindowsDoesNotDoubleJoin` (line 61) to assert
   `p.LogDir == r.Logs` and that `strings.Count(p.LogDir, "mcremote") == 1`
   on the product leaf.
5. `internal/appdirs/paths_test.go`:
   * add `TestResolveLogDirScopedRootUnchanged`: `r := testRoots(t)`,
     `r.Logs = filepath.Join(r.DataHome, "prod", "Logs")`, `r.ProductScoped = true`
     → `p.LogDir == r.Logs` (the Windows shape, OS-independent);
   * add `TestResolveLinuxLogDirEqualsStateDir` (F17 pin): construct roots with
     `ProductScoped: false` and `Logs == StateHome` → assert
     `p.LogDir == p.StateDir`; the test is deterministic on every OS because it
     builds its own `Roots`;
   * keep `TestResolveLeavesLogDirEmptyWithoutLogsRoot` (line 211) unchanged —
     it builds its own roots with `r.Logs = ""`, so it survives the unix change.

**Verification:**

```bash
go test ./internal/appdirs/... -count=1
go run ./cmd/mcremote paths --json   # Windows: "…\\mcremote\\Logs" once; Linux: "…/.local/state/mcremote" == state_dir
```

### P2 — The rotator (D6, D7, D9; closes F12; obeys C3, C8)

New `internal/logging/rotate.go` (package `logging`), no imports outside the
standard library and `internal/appdirs` (no cycle: `appdirs` does not import
`logging`).

```go
type RotateConfig struct {
    Filename   string           // required, absolute
    MaxSize    int64            // bytes; 0 = unbounded
    MaxBackups int              // 0 = unbounded
    MaxAge     time.Duration    // 0 = unbounded
    Compress   bool
    Now        func() time.Time // nil = time.Now (test seam)
    OnError    func(error)      // nil = one-line warning on os.Stderr (test seam)
}

type RotatingFile struct { /* mutex, *os.File, size int64, cfg, reported map[string]struct{} */ }

func OpenRotatingFile(cfg RotateConfig) (*RotatingFile, error)
func (f *RotatingFile) Write(p []byte) (int, error)
func (f *RotatingFile) Close() error
```

Semantics (exactly; each is pinned by a named test):

1. **Open.** `OpenRotatingFile` validates `Filename != ""` and absolute, calls
   `appdirs.EnsurePrivateDir(filepath.Dir(Filename))`, opens
   `O_CREATE|O_APPEND|O_WRONLY` mode `0600`, records `Stat().Size()`, and
   rotates once if `MaxSize > 0 && size >= MaxSize`. An open error propagates
   to the caller — D8 decides fatality, not the rotator.
2. **Write.** Lock. If `MaxSize > 0 && f.size > 0 && f.size+len(p) > MaxSize`,
   call `rotate()`; on rotation error, report it (step 6) and, if the file is
   closed, reopen the current filename `O_CREATE|O_APPEND|O_WRONLY`, then write
   `p` regardless. `size == 0` never rotates, so a single record larger than
   the cap lands in a fresh file whole (D6). The only error `Write` ever
   returns is the underlying `file.Write` error (disk full); rotation, prune,
   and compress errors never reach the caller (C3).
3. **Rotate.** Close the file; compute the backup name
   `<base>-<UTC 2006-01-02T15-04-05.000>.log` from `Now()`; while that name
   exists, retry with `-1`, `-2`, … inserted before the extension (C8 —
   Windows `os.Rename` replaces silently, F16; the loop is bounded by
   `MaxBackups+2` attempts, then the rotation fails and step 2's fallback
   applies); `os.Rename`; open a fresh `O_CREATE|O_TRUNC|O_WRONLY` `0600`
   active file; `size = 0`; then prune and compress. A failed rename reopens
   the original for append and returns the rename error to `Write`'s
   best-effort handler.
4. **Prune.** `os.ReadDir(dir)`; select names matching
   `<base>-<ts><suffix?>.log` or `…log.gz` where `<ts>` parses as
   `2006-01-02T15-04-05.000` and `<suffix?>` is an optional `-N`; sort newest
   first; remove beyond `MaxBackups`; remove older than `MaxAge` (using the
   parsed timestamp, not mtime); if `Compress`, gzip each uncompressed backup
   to `<name>.gz` and remove the original. Removal of an open file is safe on
   Windows (F16, probe 6). Prune errors are reported (step 6), never returned.
5. **Close.** Flushes nothing extra (unbuffered `*os.File`), closes the handle,
   is idempotent (second call returns nil), and leaves the struct unwritable
   (subsequent `Write` reopens lazily per step 2).
6. **Error reporting.** `OnError` fires once per distinct error string
   (`reported` set under the same mutex); default writes
   `mcremote log rotation: <err>` to `os.Stderr`. "Once" means once per
   distinct message, not once per process (D7's "reports the failure once").
7. **Test seams.** Package vars `nowFn = time.Now` and `renameFile = os.Rename`
   exist only for tests; production leaves them at their defaults. `Now` and
   `OnError` in the config are the primary seams; the package vars exist solely
   to simulate a failed rename.

`internal/logging/rotate_test.go` (all deterministic via `Now`/`renameFile`/temp dirs):

```text
TestRotateAtSize                  tiny MaxSize; N writes → active + ≥1 backup, name matches <base>-<ts>.log
TestRotateOnOpenOverSize          pre-write an oversized active file; Open → rotated before the first write
TestAppendAcrossReopen            write, Close, Open, write → both payloads present in the active file
TestMaxBackupsPruned              8 rotations, MaxBackups=2 → exactly 2 backups remain
TestMaxAgePruned                  advance Now past MaxAge → old backups removed (by parsed timestamp)
TestCompressProducesGz            Compress=true → backup is .log.gz and gunzips to the original bytes
TestRotationFailureKeepsWriting   renameFile errors → later writes still land; OnError called; Write err == nil
TestSingleRecordOverCap           one write > MaxSize with size==0 → payload fully present, not dropped
TestConcurrentWrites              16 goroutines × 100 lines → line count exact, no partial line (run with -race)
TestNoRotationWhenUnbounded       MaxSize=0, MaxBackups=0, MaxAge=0 → one file, no backup
TestBackupNameCollisionSuffixed   pre-create the timestamped target → rotation yields <base>-<ts>-1.log; both backups survive (C8)
TestPruneParsesSuffixedNames      suffixed backups are counted and pruned like plain ones
TestOnErrorDeduped                same failing rename twice → OnError fires once per distinct message
TestCloseIdempotent               Close twice → second returns nil; Write after Close reopens and lands
```

**Verification:**

```bash
go test -race ./internal/logging/ -run 'Rotate|Concurrent|Unbounded|Collision|Prune|OnError|Close|Append|Single' -count=1 -v
```

### P3 — Config, env, and flag surface for both products (D4, D5; closes F14; obeys C2, C9)

**Keys, defaults, validation (identical for both products):**

| Config | Env (mcremote / mcrelay) | Flag | Default | Validate |
| --- | --- | --- | --- | --- |
| `log.file` | `MCREMOTE_LOG_FILE` / `MCRELAY_LOG_FILE` | `--log-file` | `""` | no `\n`, `\r`, `\x00`; `"off"` is the disable sentinel |
| `log.max_size_mb` | `…_LOG_MAX_SIZE_MB` | `--log-max-size-mb` | `10` | `>= 0` (0 = unbounded) |
| `log.max_backups` | `…_LOG_MAX_BACKUPS` | `--log-max-backups` | `5` | `>= 0` (0 = unbounded) |
| `log.max_age_days` | `…_LOG_MAX_AGE_DAYS` | `--log-max-age-days` | `28` | `>= 0` (0 = unbounded) |
| `log.compress` | `…_LOG_COMPRESS` | `--log-compress` | `true` | — |

**Resolution helpers** (both products, same semantics):

```go
// LogFilePath resolves log.file: "off" (case-insensitive) → "" (no file sink);
// "" → filepath.Join(Paths.LogDir, "<product>.log"), or "" when LogDir is
// empty; anything else → verbatim, made absolute against the working directory
// (filepath.Abs) so the rotator's absolute-path precondition holds.
func (c Config) LogFilePath() string
// LogFileRequired is true only when the operator set an explicit path
// (log.file is neither "" nor "off"). D8: required → startup error on open
// failure; default → warn and continue stderr-only.
func (c Config) LogFileRequired() bool
```

**mcremote** (`internal/config/config.go`, `internal/config/load.go`,
`internal/cli/root.go`):

1. Extend `LogConfig` (config.go:416–420) with the five fields:
   `File string \`mapstructure:"file"\``, `MaxSizeMB int \`mapstructure:"max_size_mb"\``,
   `MaxBackups int \`mapstructure:"max_backups"\``,
   `MaxAgeDays int \`mapstructure:"max_age_days"\``,
   `Compress bool \`mapstructure:"compress"\``.
2. `Defaults()` `Log` literal (config.go:797–800): add
   `File: "", MaxSizeMB: 10, MaxBackups: 5, MaxAgeDays: 28, Compress: true`.
3. `setDefaults` (load.go:280–281): five `v.SetDefault("log.*", …)` lines
   after the existing two. Required by C9 — without them the zero-valued flag
   defaults are the only fallback.
4. `BindEnv` (load.go:45–46): the five `MCREMOTE_LOG_*` bindings after the
   existing two.
5. `bindFlags` mappings (load.go:402–426): `"log-file"`, `"log-max-size-mb"`,
   `"log-max-backups"`, `"log-max-age-days"`, `"log-compress"` → the five keys.
   Mechanism is `Changed`-gated `BindPFlag` (load.go:435, F18) — untouched.
6. `internal/cli/root.go` persistent flags (beside 88–89): register the five
   with zero-value defaults (`String("log-file", "", …)`,
   `Int("log-max-size-mb", 0, …)`, `Int("log-max-backups", 0, …)`,
   `Int("log-max-age-days", 0, …)`, `Bool("log-compress", false, …)`).
   No package vars are needed: unlike `logLevel`/`logFormat` (vars at 20–22,
   consumed by `runSetupService`), nothing outside viper reads these; the
   help text names the env var, matching the style at 88–89.
7. `Validate` (config.go:1119–1128): after the `log.format` switch, reject a
   `log.file` containing `\n`, `\r`, or `\x00` (message names `log.file`), and
   reject `MaxSizeMB < 0`, `MaxBackups < 0`, `MaxAgeDays < 0` (each message
   names its key and says `0 means unbounded`).
8. Add `LogFilePath`/`LogFileRequired` (spec above, product leaf
   `mcremote.log`).

**mcrelay** (`internal/relay/fileconfig.go`, `internal/relay/cli.go`) mirrors
all eight items at its own anchors: `LogConfig` (fileconfig.go:143–147),
`DefaultsFile()`, `setFileDefaults` (476–512, beside 480–481), `BindEnv`
(230–231), `bindRelayFlags` pairs (514–529, beside 519–520; bound via
`BindPFlag` at 545), `Validate` (553–569, after the `log.format` switch),
persistent flags in `newRootCmd` (beside 89–91), and
`LogFilePath`/`LogFileRequired` on `FileConfig` with leaf `mcrelay.log`
(`Paths` field: fileconfig.go:38). **No manual post-Load override is added**:
the existing one at cli.go:233–238 exists for the root-owned
`logLevel`/`logFormat` pointers; the new knobs travel entirely through
`bindRelayFlags` like `listen-host`/`data-dir` do.

**Tests** — `internal/config/config_test.go` and
`internal/relay/fileconfig_test.go`, mirrored:

* each of the five keys resolves from YAML, from env (`t.Setenv`), and from a
  Changed flag — and flag beats env beats config beats default;
* defaults are `10/5/28/true` with `file: ""` (from `Defaults()`/`DefaultsFile()`);
* **C9 pin**: Load with the five flags registered but untouched yields the
  defaults, not `0/0/0/false`;
* `--log-max-size-mb 0` (Changed) yields `0`, not the default `10`;
* the three invalid `log.file` control-character values and each negative int
  are rejected with a message naming the key;
* `LogFilePath`: `"off"`/`"OFF"` → `""`; `""` → `<LogDir>/<product>.log`;
  explicit relative → absolute; `LogFileRequired` true only for the explicit case.

**Verification:**

```bash
go test ./internal/config/ ./internal/relay/ -run 'Log|Validate|Default|Flag' -count=1
go run ./cmd/mcremote serve --help | Select-String 'log-file|log-max-size-mb|log-max-backups|log-max-age-days|log-compress'
go run ./cmd/mcrelay  serve --help | Select-String 'log-file|log-max-size-mb'
```

### P4 — Wire the sink into both daemons (D1, D2, D8, D13; closes F1, F2, F3, F6, F10; obeys C4)

1. `internal/logging/slog.go`:
   * `Setup` (20–40) keeps its exact behaviour: handler over `opts.Out` or
     `os.Stderr`. It remains the whole API for every non-serve caller.
   * Extend `Options` with: `File string`, `RequireFile bool`,
     `MaxSizeBytes int64`, `MaxBackups int`, `MaxAge time.Duration`,
     `Compress bool`, `OnError func(error)`. `Out` keeps its meaning — the
     **non-file** sink (default `os.Stderr`).
   * Add:

     ```go
     // Open builds a logger and, when opts.File is set, a rotating file the
     // caller must Close. On failure: a required file is a startup error; a
     // default file warns once on the non-file sink and falls back to
     // non-file-only logging (D8).
     func Open(opts Options) (*slog.Logger, io.Closer, error)
     ```

     `Open` semantics: `File == ""` → `Setup(opts)` + `noopCloser` + nil
     error. `File != ""` → `OpenRotatingFile(RotateConfig{Filename: File,
     MaxSize: MaxSizeBytes, MaxBackups, MaxAge, Compress, OnError})`; on
     success set the handler writer to `teeWriter{file: rf, other: <opts.Out
     or os.Stderr>}` and return `rf` as the closer; on error with
     `RequireFile` → return it; on error without → write one warning line to
     the non-file sink and degrade to `Setup(opts)`.
   * `teeWriter.Write(p)`: write `p` to `other` **ignoring its result**, then
     return `file.Write(p)`. Deliberately not `io.MultiWriter`: MultiWriter
     aborts at the first failing writer, so either a failing file would
     swallow the stderr copy (violating C4's journald/launchd driver) or — in
     the order the MADR sketches — a detached Windows daemon's readerless
     `stderr` failing would swallow the file copy, defeating the entire
     record. `teeWriter` is D2's "or an equivalent".
2. `internal/cli/serve.go`: replace the `logging.Setup` call (80–83) with

   ```go
   opts := logSinkOptions(cfg, "mcremote")
   logger, closer, err := logging.Open(opts)
   if err != nil { return err }
   defer closer.Close()
   ```

   and add the unexported mapping helper (unit-testable, no I/O):

   ```go
   func logSinkOptions(cfg config.Config, product string) logging.Options {
       // Level/Format from cfg.Log; File: cfg.LogFilePath();
       // RequireFile: cfg.LogFileRequired();
       // MaxSizeBytes: int64(cfg.Log.MaxSizeMB) << 20;
       // MaxBackups: cfg.Log.MaxBackups;
       // MaxAge: time.Duration(cfg.Log.MaxAgeDays) * 24 * time.Hour;
       // Compress: cfg.Log.Compress.
   }
   ```

   The startup line `"starting mcremote"` (serve.go:102) is emitted through
   this logger and is the string P7 asserts in the file.
3. `internal/relay/cli.go`: replace the `logging.Setup` call (257–260) with the
   same three-line shape using an equivalent `logSinkOptions` over
   `FileConfig` (`fc.LogFilePath()`, `fc.LogFileRequired()`, `fc.Paths.LogDir`,
   product leaf `mcrelay.log`) and `defer closer.Close()`. The startup line is
   `"mcrelay starting"` (cli.go:292).
4. No change to `daemon.Run`; it keeps converging `DataDir`/`ConfigDir` only
   (daemon.go:86–106). The rotator converges the log directory itself via
   `EnsurePrivateDir` (P2 step 1), which is what closes F3 without touching
   the daemon.

**Tests** — `internal/logging/slog_test.go`:

* `TestOpenNoFile`: `File: ""` → closer is a no-op, logger writes to the
  injected `Out`;
* `TestOpenWritesFileAndStderr`: temp `File` + buffer `Out` → one `Info`
  appears in both the file and the buffer (C4);
* `TestOpenRequireFileFails`: unwritable path (a file as parent dir) +
  `RequireFile: true` → non-nil error;
* `TestOpenDefaultPathWarnsAndFallsBack`: unwritable path,
  `RequireFile: false` → nil error, logger works, warning reached the buffer,
  no file created;
* `TestTeeWriterIgnoresOtherFailure`: a failing `Out` (errorWriter) → the file
  still receives every record and `Write` returns nil.

`internal/cli/serve_test.go`: `TestLogSinkOptions` table — `""` → default path
under `cfg.Paths.LogDir` with `RequireFile=false`; `"off"` → `File:""`,
`RequireFile=false`; explicit → verbatim absolute with `RequireFile=true`;
`MaxSizeBytes == MB<<20`; `MaxAge == days*24h`.

**Verification:**

```bash
go test -race ./internal/logging/ ./internal/cli/ -run 'Open|Tee|LogSink|Serve' -count=1 -v
```

Then, once by hand (the live behaviour unit tests cannot cover): run
`go run ./cmd/mcremote serve --tls=false --log-file "$env:TEMP\m.log"` in the
foreground, see `starting mcremote` on the terminal, confirm the file contains
it, Ctrl-C, confirm no panic and the file is complete. Repeat with
`--log-file off` → no file created. Repeat with
`--log-file <unwritable>` → startup fails with an error naming the path (D8).

### P5 — Service reporting, relay paths, and doctor (D10, D11, D14; closes F7, F8, F9)

1. `internal/cli/service/setup.go`:
   * In `Setup`, after options normalization and before the `installOS`
     dispatch (300–325), resolve
     `p, perr := resolveProductPaths(opts.Product, opts.DataDir)` (helper
     exists, setup.go:1030) and, when `perr == nil`, set
     `res.LogDir = p.LogDir` for **every** OS. A resolution failure leaves
     `LogDir` empty — reporting must never fail setup.
   * In `setupLaunchdAgent` (431–435), replace the hardcoded
     `filepath.Join(home, "Library", "Logs", opts.Product)` with the resolved
     `res.LogDir` (keep the `MkdirAll`), so the plist stdio directory
     (`plist_render.go:56–61`) and the daemon's own file cannot diverge (D10).
   * Update the `Result.LogDir` doc comment (setup.go:112–113): "resolved log
     directory on every platform; empty only when resolution failed".
2. `internal/cli/service/result_print.go`: add `case "windows-task":` beside
   `launchd-agent` in all three switches — display (48–57), linger/logs
   (95–109), status (118–135). The status block prints:

   ```text
   Status:  schtasks /query /tn <res.Label> /fo LIST /v
   Logs:    <res.LogDir>\<product>.log
   Tail:    Get-Content -Wait "<res.LogDir>\<product>.log"   (re-attach after a rotation; it follows the old file)
   Stop:    schtasks /end /tn <res.Label>
   Remove:  <product> setup-service --remove
   ```

   and the display block prints `Scope: windows-task (Task Scheduler)` plus
   `Logs dir:` when `res.LogDir != ""` (mirror 98–100). No `journalctl` line
   may appear for this scope.
3. `internal/relay/cli.go` `newPathsCmd`: add `"log_dir": cfg.Paths.LogDir` to
   the JSON map (126–138) only when non-empty (mcremote uses `omitempty`,
   paths.go:68 — mirror the behaviour), and a conditional
   `log_dir:            %s` text line beside 140–150, matching mcremote's
   `printPaths` (paths.go:91–93) including the column alignment.
4. `internal/cli/doctor.go`: `newDoctorCmd`'s `RunE` (23–31) gains, after
   `renderCredentialDoctor(w, probeCredentialStores())` (line 29):

   ```go
   cfg, err := config.Load(config.LoadOptions{ConfigFile: cfgFile, Flags: cmd.Flags()})
   if err == nil {
       fmt.Fprintln(w)
       renderLogDoctor(w, cfg.LogFilePath(), probeLogFile(cfg.LogFilePath()))
   }
   ```

   (`cfgFile` is the package var at root.go:20; the Load call is the one
   paths.go:23–26 already uses, so doctor and paths cannot disagree.) A Load
   error skips the section silently — doctor's other sections still render.

   ```go
   type logFileStatus struct { Disabled bool; Present bool; Size int64; Writable bool; Err string }
   // probeLogFile: path == "" → Disabled. Else os.Stat (Present, Size) and an
   // O_WRONLY|O_APPEND open attempt, closed immediately (Writable); it never
   // writes a byte. Any error is summarized in Err, not returned.
   func probeLogFile(path string) logFileStatus
   // renderLogDoctor prints: resolved path (or "disabled via log.file=off"),
   // present yes/no, size in bytes, writable yes/no, and Err when set.
   func renderLogDoctor(w io.Writer, path string, st logFileStatus)
   ```

**Tests:**

* `internal/cli/service/setup_test.go`: with `OverrideInstallOS` (setup.go:202)
  set to each of `linux`, `darwin`, `windows` and `PrintOnly: true` (so no
  install runs), assert `res.LogDir` is non-empty; for `windows` assert it
  ends in `mcremote\Logs` with one product leaf.
* `internal/cli/service/result_print_test.go` (new): `PrintSetupResult` with
  `Scope: "windows-task"` and a `LogDir` → output contains the log path,
  `Get-Content -Wait`, `schtasks /query`, and **not** `journalctl` or
  `systemctl`.
* `internal/relay/cli_test.go`: extend the paths JSON/text tests — non-empty
  `LogDir` appears as `log_dir` in both; empty `LogDir` appears in neither.
* `internal/cli/doctor_test.go` (file exists; extend): `TestProbeLogFile`
  (absent path, existing file, path whose parent is a file → `Err` set,
  `""` → `Disabled`) and `TestRenderLogDoctor` golden-substring checks for
  each state.

**Verification:**

```bash
go test ./internal/cli/service/ ./internal/cli/ ./internal/relay/ -run 'LogDir|PrintSetup|Doctor|Paths|ProbeLog' -count=1 -v
go run ./cmd/mcremote doctor          # new "log file" section names the path
go run ./cmd/mcrelay  paths --json | Select-String log_dir
```

### P6 — Docs, defaults, and examples (D12; closes F15; keeps C6)

1. `docs/config.md`: five rows in the defaults table beside `log.level`/
   `log.format` (106–107); five `MCREMOTE_LOG_*` rows in the env table beside
   409–410; five rows in the flag reference beside `--log-level`/`--log-format`
   (546–547). State the D5 defaults, the `off` sentinel, `0 = unbounded`, and
   that the active file is `<log_dir>/mcremote.log` with per-platform
   `log_dir` from `mcremote paths`.
2. `docs/config-mcrelay.md`: the same three edits beside 69–70 (combined
   default/env/flag table), 145–146 (env), 324–325 (flags); leaf
   `mcrelay.log`.
3. `docs/ops-windows-install.md`: layout table (51–58) — the `Logs` row gains
   the active file: `%LocalAppData%\mcremote\Logs\mcremote.log`. Add a short
   "Logs" subsection after the table: `Get-Content -Wait` tail command, the
   F16 caveat (after a rotation the tail follows the renamed backup —
   re-attach), the five rotation knobs with env/flag names, and one sentence
   on where rotated backups live and how they are named/compressed.
4. `internal/cli/service/defaults_mcremote.yaml` (log block at 27–29): add the
   five keys with D5 defaults. This file's `log:` block carries **no** inline
   comments — match that. `internal/cli/service/defaults_mcrelay.yaml` (45–47)
   uses trailing `# ENV / --flag` comments — match that instead. Each file
   keeps its own house style.
5. `internal/cli/examples.go`: line 176 becomes
   `tail -f ~/Library/Logs/mcremote/mcremote.log` (the daemon-owned file, not
   launchd's `.err.log`); add one `--log-file` example beside the
   `--log-level`/`--log-format` examples at 14–16 and 55–56.

**Verification:**

```bash
go test ./internal/cli/service/ ./internal/cli/ -run 'Default|Example' -count=1
Select-String -Path docs\config.md,docs\config-mcrelay.md -Pattern 'max_size_mb|log.file' | Measure-Object   # ≥ 10 rows total
Select-String -Path internal\cli\service\defaults_mcremote.yaml,internal\cli\service\defaults_mcrelay.yaml -Pattern 'max_size_mb|compress'
Select-String -Path docs\ops-windows-install.md -Pattern 'mcremote\.log|Get-Content -Wait'
```

### P7 — Windows acceptance (D1, D12; closes F2, F4 on the host; re-pins F16; answers OQ2)

All additions to `scripts/acceptance-windows.ps1`, after the `paths --json
(C1)` block (97–144; `Assert-SamePath` at 61–67). The existing conditional
`log_dir` assert (142–144) becomes unconditional: `log_dir` is now always
emitted, and the condition would silently skip the check if emission ever
regressed. Keep the script Windows-only (`exit 0` off-Windows, guard 25–34)
and honour `-SkipTests` (param, line 20) for the functional blocks below.

1. **C1 hardening:** replace 142–144 with
   `Assert-SamePath $j.log_dir (Join-Path $local 'mcremote\Logs') 'log_dir'`
   guarded only by `Invoke-Check`.
2. **Functional file check:** create `$tmpLog`/`$tmpData` under `$env:TEMP`,
   pick a free high port, start
   `& $bin serve --tls=false --listen-host 127.0.0.1 --listen-port $port --data-dir $tmpData --log-file $tmpLog`
   as a background process, poll up to 15 s for `$tmpLog`, assert it exists and
   contains `starting mcremote` (the literal from serve.go:102), then stop the
   process and remove the temp dirs in a `finally`.
3. **`off` check:** the same short run with `--log-file off` creates no file.
4. **Rotation + steady-state measurement (OQ2):** run with
   `--log-max-size-mb 1 --log-max-backups 2 --log-compress`, drive output
   (e.g. `--log-level debug` plus a client connection or two), wait for ≥1
   backup, then assert: ≤2 backups, backups match
   `mcremote-*.log(.gz)` (suffix `-N` allowed), and **record the observed
   bytes** of active + backups with `Write-Host` so the transcript carries the
   number the MADR asks for; the run is the measurement, not a synthetic file.
5. **Rename-while-tailed re-pin (C7):** while `Get-Content -Wait $tmpLog` is
   attached, force a rotation (step 4's run). **Expected per F16 (3/3 host
   runs): the rename succeeds.** Assert the new active file exists and keeps
   receiving lines. If — on some future Windows build — the rename fails, the
   assertion is instead D7's fallback: the daemon kept logging to the same
   file and emitted exactly one `log rotation` warning; record which branch
   ran in the transcript and add a one-line note to
   `docs/ops-windows-install.md`. Never delete this probe; a silent
   behaviour change here is the failure mode C7 exists for.

**Verification (Windows host):**

```powershell
make ci-windows            # GNUmakefile → make/ci-windows.mk → scripts/ci-windows-local.ps1
scripts\acceptance-windows.ps1
```

## Verification (whole plan)

```bash
make test
make race
make lint
make tidy
git diff --exit-code go.mod go.sum   # C1
```

(`make preflight` additionally runs the mobile trio; run it before push per
AGENTS.md. `make ci-windows` / `ci-windows-smoke` skip with exit 0 off-Windows.)

On the Windows host, additionally:

```powershell
make ci-windows
make ci-windows-smoke
scripts\acceptance-windows.ps1
```

**Not verifiable on a non-Windows host:** P7's functional blocks and the Task
Scheduler registration. **Not verifiable on a non-macOS host:** the launchd
regression check (`launchctl print`, `~/Library/Logs/mcremote/mcremote.err.log`
still receiving lines). Both are stated here rather than skipped silently; P7
records their results. The rename-while-tailed behaviour **is** verified on
this host already (F16 probe, 2026-09-13); P7 re-pins it on the acceptance
machine.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `mcremote paths --json` and `mcrelay paths --json` emit the D3 `log_dir` on the host OS, product leaf once | D3, D11 |
| A2 | `mcremote serve` creates `<log_dir>/mcremote.log`, appends across a restart, and closes on SIGTERM | D1, D13 |
| A3 | The same run's records also reach `stderr`; a failing file does not silence `stderr` and a failing `stderr` does not silence the file | D2, C4 |
| A4 | With `max_size_mb=1`, `max_backups=2`, `compress=true`: rotation happens, exactly ≤2 backups remain, backups end `.log.gz` | D5, D6 |
| A5 | A forced rotate failure keeps writing, warns once per distinct error, and returns no write error (`TestRotationFailureKeepsWriting`, `TestOnErrorDeduped`) | D7 |
| A6 | `log.file: "off"` (and `--log-file off`) creates no file | D5 |
| A7 | An explicitly set, unopenable `log.file` fails startup; the unopenable default warns and continues | D8 |
| A8 | Windows `setup-service` prints the log path, `Get-Content -Wait` with the re-attach caveat, and no `journalctl` line | D10, F16 |
| A9 | `mcremote doctor` names the log path, size, and writability | D14 |
| A10 | The five keys resolve from config, env, and flag for both products; invalid values are rejected; flag zero-defaults do not shadow config defaults (C9 pin) | D4, F18 |
| A11 | `go.mod`/`go.sum` are unchanged | D9 |
| A12 | `scripts/acceptance-windows.ps1` passes on the Windows host, including the hardened `paths --json (C1)` | D12 |
| A13 | `TestResolveLinuxLogDirEqualsStateDir` pins the F17 overlap; Linux `paths` shows `.log_dir == .state_dir` | D3, F17 |
| A14 | P7's transcript records the observed active-plus-backups byte total from a real rotation run | OQ2 |
| A15 | A backup-name collision produces a `-N` suffix and never overwrites an existing backup (`TestBackupNameCollisionSuffixed`) | D6, F16, C8 |
| A16 | The rename-while-tailed branch actually taken on the acceptance host is recorded in P7's transcript | F16, C7 |

Most likely to be quietly dropped: **A12/A14/A16**, because they are the only
criteria that cannot fail on CI and need the physical Windows host. It would be
tempting to declare the plan done on a green `make test`; the plan is not done
until the Windows script's transcript (or an explicit statement that the host
run is scheduled) records all three.

## Rollout and Rollback

What a user observes: after the first daemon start, a new file appears at the
documented `log_dir`; it grows, rotates, and gzips; `stderr`/journald/launchd
behaviour is unchanged; `setup-service` and `doctor` name the file; on Linux
the file lives inside the state directory (documented, F17). `log.file: "off"`
restores a file-free daemon in one setting.

Rollback is per-phase `git revert` of the phase commit; no migration runs and
no state format changes. Reverting P4 alone stops file creation everywhere
while leaving the path fix (P1) and reporting (P5) harmless.

## Deferred (named, so they are not mistaken for oversights)

* **SIGHUP / manual rotation** (MADR OQ3, closed as deferred) — waits for a
  second record; rotation-on-size already bounds the file, and Windows has no
  SIGHUP, so the signal surface differs per OS and needs its own decision.
* **Instance-keyed filenames** (D6) — per-product matches existing macOS
  behaviour; a second `--data-dir` instance shares the file. Belongs to a
  follow-up if multi-instance hosts become supported.
* **zstd compression** — gzip is stdlib and sufficient; zstd would add a
  module (C1) for marginal gain.
* **`doctor` content scanning / error summarising** (D14 limits it to path,
  size, writability) — a different feature with its own privacy review.
* **Windows Event Log mirroring** — the file is the contract; an Event Log
  sink is a separate integration.
* **Removing the launchd/journald capture redundancy** — the overlap is what
  keeps certmagic and panic output reachable (F6); revisiting it needs a
  record of its own.
