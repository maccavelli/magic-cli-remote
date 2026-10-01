---
status: proposed
date: 2026-09-13
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Daemon-owned rolling logs: each daemon writes, rotates, and bounds its own log file on every platform

## Context and Problem Statement

`mcremote serve` on Windows produces no log file at all, and the directory the
documentation promises it would live in is not created. The daemon writes its
structured log only to `os.Stderr`; on Linux and macOS the service manager
happens to capture that stream, but the Task Scheduler task that runs the daemon
on Windows has no console and discards it. The daemon therefore loses its entire
operational record exactly where it is hardest to reproduce.

The problem is not "Windows is missing a redirect". Nothing in the tree opens a
log file on any platform. Logs exist on Linux and macOS only because systemd and
launchd were configured to catch a stream the daemon emits — the daemons do not
own their logs, the service manager does. This record makes the daemon own them,
uniformly, with rotation and retention, and leaves the service managers'
capture in place as a redundant channel rather than the source of truth.

### What was measured, not assumed

**No code path opens a log file.** A tree-wide search for `lumberjack`,
`rotat…`, `O_APPEND`, `OpenFile` outside lock files, and `log_file`/`LogFile`
finds no producer. `logging.Options.Out` is the only sink and defaults to
`os.Stderr` (`internal/logging/slog.go:15`, `20-24`). The two daemons that
configure it are `mcremote serve` (`internal/cli/serve.go:80-83`) and
`mcrelay serve` (`internal/relay/cli.go:257-260`); neither sets `Out`.

**The Windows task discards the stream.** The rendered task action carries only
`<Command>`, `<Arguments>`, and `<WorkingDirectory>` — no redirection
(`internal/cli/service/schtasks.go:126-129`; consumed by
`internal/cli/service/setup_schtasks.go:32-49`). A Task Scheduler process runs
detached with no console, so the inherited `stderr` has no reader.

**Windows rotation semantics are measured on this host, 3/3 identical runs
(2026-09-13, go1.26.6 windows/amd64).** A probe program exercising the exact
rotator operations — with a live `powershell Get-Content -Wait` reader attached
to the active file — established: `os.Rename` of the tailed active file
**succeeds**; recreating the active file `O_CREATE|O_APPEND|O_WRONLY 0600` and
writing to it succeeds; a second rotation succeeds while the reader still holds
the first backup; `os.Rename` **onto an existing target succeeds** (Go issues
`MoveFileEx` with `MOVEFILE_REPLACE_EXISTING`, so a backup-name collision is a
silent overwrite, not an error); `os.Remove` of a backup the reader is holding
succeeds (prune can never be blocked by a tail); and the reader process survives
rotation but follows its **handle** — after a rename, `Get-Content -Wait` keeps
reading the backup, not the new active file, so an operator's tail must be
re-attached after each rotation.

**Nothing creates the log directory.** `daemon.Run` converges only `DataDir` and
`ConfigDir` (`internal/daemon/daemon.go:86`, `100-106`). The Windows path
(`setupSchtasks`) never creates `Paths.LogDir`, unlike the macOS path, which
does (`internal/cli/service/setup.go:431-435`).

**The resolved Windows log path contradicts the documentation and the
acceptance script.** On this Windows host:

```powershell
> go run ./cmd/mcremote paths --json
{
  "product": "mcremote",
  ...
  "log_dir": "C:\\Users\\<user>\\AppData\\Local\\mcremote\\Logs\\mcremote",
  ...
}
```

`docs/ops-windows-install.md:58` documents `%LocalAppData%\mcremote\Logs`, and
`scripts/acceptance-windows.ps1:142-143` asserts exactly that against a strict
`OrdinalIgnoreCase` comparison (`Assert-SamePath`, `:61-67`). The resolved path
carries a second `\mcremote` leaf. The cause is real: `Roots.Logs` is already
product-scoped on Windows (`internal/appdirs/roots_windows.go:64`, with
`ProductScoped: true` at `:67`), but `Resolve` appends the product leaf with a
raw `filepath.Join` instead of `roots.joinProduct`
(`internal/appdirs/paths.go:70`; the helper is `internal/appdirs/roots.go:27-35`).
The Windows acceptance check `mcremote paths --json (C1)` therefore fails. It
has never been observed because the assertion landed 2026-09-06 (`git blame`
`ca436bbc`) on a script that only runs on the owner's Windows laptop.

**Linux has no log root by deliberate decision.** `Roots.Logs` is set only on
Darwin (`internal/appdirs/roots_unix.go:48-54`), and that is pinned by
`TestSystemRootsDispatches` (`internal/appdirs/systempaths_test.go:38-41`,
"only darwin has a stdio base (MADR 0116 F4)"). A unified default requires
adding an XDG-correct Linux root and amending that finding.

**The XDG-correct Linux log root collides with `StateDir`.** `StateDir` is
`joinProduct(StateHome, product)` = `$XDG_STATE_HOME/mcremote`
(`internal/appdirs/paths.go:59`, `roots_unix.go:44`). Setting the `Logs` base to
`$XDG_STATE_HOME` and resolving `LogDir` with the same `joinProduct` rule yields
the identical directory, so on Linux — and only Linux — the log file lives
beside the state tree (`instances/…`). The rotator's prune scan is prefix- and
suffix-gated (`<product>-<timestamp>.log[.gz]`) and cannot match state content,
so the overlap is inert; it is nonetheless a visible layout fact and must be
documented and pinned by a test rather than discovered later.

**Flag binding is `Changed`-gated, so zero-valued flag defaults cannot shadow
viper defaults.** Both products bind flags with `v.BindPFlag`
(`internal/config/load.go:435`, `internal/relay/fileconfig.go:545`); viper only
prefers the flag value when `flag.HasChanged()`, and falls back to its own
default otherwise. An `IntVar` flag registered with default `0` therefore does
not clobber `SetDefault("log.max_size_mb", 10)`, while an explicit
`--log-max-size-mb 0` still means "unbounded". mcrelay's persistent flags reach
`serve` through the inherited `cmd.Flags()` passed to `Load`
(`internal/relay/cli.go:217-221`).

**The service manager's capture is not a superset of what we want.** certmagic's
logger writes straight to `os.Stderr` through zap
(`internal/certs/acme.go:191-203`), bypassing `slog`. A file sink built on
`slog`'s `Out` would miss those lines; a redirect would miss nothing but cannot
exist on Windows. Neither channel subsumes the other.

**`setup-service` misreports Windows as systemd.** `result_print.go` has no
`windows-task` case, so Windows falls into `default` and prints
`Unit name: … .service`, `Logs: journalctl --user -u …`, and
`Disable: systemctl …` (`internal/cli/service/result_print.go:44-56`, `95-109`,
`118-135`). `Result.LogDir` is assigned only by the launchd path and is
documented as "macOS log directory; empty on Linux"
(`internal/cli/service/setup.go:112-113`, `431-435`).

**mcrelay shares the gap and hides the path.** It uses the same
`logging.Setup` call, and its `paths` output omits `log_dir` in both JSON and
text (`internal/relay/cli.go:126-138`, `140-150`).

**CI enforces module hygiene.** `go mod tidy` must be byte-clean
(`.github/workflows/ci.yml:94-104`); `gofmt`, `go vet`, and `go test -race ./...`
gate the Go job (`:84-92`, `:106-115`). `Makefile` provides `tidy`, `lint`,
`vulncheck`, `race`, and `preflight`.

### Findings

**F1 — No daemon owns a log file on any platform.** The only sink is `stderr`
(`internal/logging/slog.go:20-24`). Linux and macOS logs are an artifact of
journald/launchd configuration, not of the daemon.

**F2 — On Windows the daemon's entire log is discarded.** The Task Scheduler
action has no redirection and no console (`schtasks.go:126-129`), and the
directory it would write into is never created.

**F3 — `Paths.LogDir` has no producer and no converger.** `daemon.Run` converges
`DataDir`/`ConfigDir` only (`daemon.go:86`, `100-106`); `setupSchtasks` creates
nothing.

**F4 — The Windows resolved log path is wrong by one leaf, and the acceptance
script already asserts the correct one.** `paths.go:70` must use
`roots.joinProduct`, or `%LocalAppData%\mcremote\Logs\mcremote` ships and
`acceptance-windows.ps1` fails.

**F5 — Linux deliberately lacks a log root, so "unify" requires amending MADR
0116 F4 and its test.** `roots_unix.go:48-54`, `systempaths_test.go:38-41`.

**F6 — `slog` output is not the whole process output.** certmagic writes to
`os.Stderr` directly (`certs/acme.go:191-203`), so the file sink is additive and
must not displace `stderr`.

**F7 — Windows `setup-service` output names service control that does not
exist.** `result_print.go:44-56`, `118-135`.

**F8 — `Result.LogDir` is populated only on macOS.** `setup.go:112-113`,
`431-435`.

**F9 — mcrelay needs the same knobs, the same sink, and a `log_dir` in `paths`.**
`relay/cli.go:257-260`, `126-150`.

**F10 — The log path is per-product, not per-instance, and that is already the
macOS status quo.** `paths.go:70` joins only the product name; the instance key
keys state/cache/runtime (`paths.go:96-99`). macOS already writes one
`<product>.out.log` per product (`internal/cli/service/plist_render.go:56-61`).
The rotator must therefore assume one writer per file, as lumberjack itself
documents.

**F11 — This project repeatedly refuses narrow new dependencies.**
MADR 0024 chose a "dependency-free package"; PLAN 0028 says "Do not add a new
external Go dependency for JSON-RPC"; PLAN 0065 says "stdlib only; no new deps";
PLAN 0029 requires each direct dependency to map to a real boundary.

**F12 — The two ready-made rotators each carry a cost.** natefinch/lumberjack
`v2.2.1` (published 2023-02-06, MIT, pure-stdlib imports, `v3` still alpha) is
size-only and feature-frozen; it documents a one-process-per-file assumption and
its `Write` returns an error both when a single write exceeds the cap and when a
rotation (`openNew`) fails. `slog`'s `Logger` methods ignore the handler error
(`handler.go:322` returns it; the caller does not observe it), so a failed
rotation silently loses the line that triggered it. DeRuina/timberjack (created
2025-04-28, MIT, 155 stars) adds time/clock rotation and gzip/zstd but is young
and single-maintainer.

**F13 — CI will reject a sloppy dependency.** `go mod tidy` diff (`ci.yml:94-104`)
plus `govulncheck` and `go test -race ./...`.

**F14 — There is no rotation policy to inherit, but explicit budgets are the
house style.** `defaultMemoryLimit = 1 << 30` is stated as a chosen ceiling with
its reasoning (`internal/cli/serve.go:143-165`). Log retention should read the
same way.

**F15 — The Windows log path is already promised to operators.** The layout
table at `docs/ops-windows-install.md:51-58` lists it; the acceptance script
checks it. This work fills a promise rather than inventing one.

**F16 — On Windows, rotation under a tail succeeds, rename-over-existing
silently replaces, and prune cannot be blocked** (host probe, 3/3 runs,
2026-09-13). Consequences: the rotator must never rely on a rename failing when
a reader is attached; it **must** avoid backup-name collisions itself, because
`MOVEFILE_REPLACE_EXISTING` would silently destroy the previous backup; and a
`Get-Content -Wait` tail follows the renamed file, so operator guidance must say
the tail needs re-attaching after a rotation.

**F17 — Under D3, Linux `LogDir` equals `StateDir`**
(`$XDG_STATE_HOME/<product>`; `paths.go:59` + the new `Logs` base). The prune
scan cannot match state content, but the overlap is a layout fact that must be
stated in the docs and pinned by a test.

**F18 — Flag/env/config precedence already behaves as D4 needs.** `BindPFlag`
is `Changed`-gated (`load.go:435`, `fileconfig.go:545`), so int/bool flags
registered with zero-value defaults coexist with `SetDefault`, and an explicit
`--log-max-size-mb 0` still selects "unbounded".

## Decision Drivers

* **The daemon, not the service manager, must be the source of truth for its
  logs** (the user's stated requirement: defaults, CLI args, or env vars — not
  schtasks/launchd/systemd). Findings F1, F2, F3.
* **One behaviour across Linux, macOS, and Windows.** The platform that exposed
  the bug must not get a special case that the others do not share. F4, F5.
* **Do not lose lines that the platform captures today.** journald/launchd and
  interactive `serve` must keep working, and non-`slog` output must still be
  reachable. F6.
* **Bounded disk use by default.** A daemon that runs for months on a laptop
  must not grow an unbounded file. F14.
* **Minimal, justified dependencies.** F11, F13.
* **The path is a contract.** `paths`, docs, and the acceptance script must
  agree. F4, F15.
* **Both products.** mcremote and mcrelay are symmetric. F9.

## Considered Options

* **A — In-tree rolling file sink.** The daemon opens, appends to, rotates, and
  prunes its own file; a small dependency-free rotator lives in
  `internal/logging`; output is teed to `stderr`.
* **B — Adopt `gopkg.in/natefinch/lumberjack.v2`.**
* **C — Adopt `github.com/DeRuina/timberjack`.**
* **D — Keep service-manager/stream redirection as the only mechanism**, adding
  a task-XML redirect on Windows and a plist/unit redirect elsewhere.
* **E — Explicit-only file sink.** No platform default; a file is written only
  when `log.file`/`--log-file` is set.

## Decision Outcome

**Chosen: A — an in-tree, dependency-free rolling file sink, on by default on
every platform, teed to `stderr`.**

A is the only option that satisfies the drivers together. B and C answer "there
is a library" but not "the log is correct": B is size-only and feature-frozen
(2023-02-06) and its `Write` error path drops records (F12); C is time-aware but
young and single-maintainer, and a rotation defect is a silent data-loss defect.
Both would still require us to pre-create a private directory, reconcile the
one-writer assumption (F10), and test the same Windows rename behaviour we must
test anyway. Against a project that has explicitly preferred dependency-free
code for narrow concerns (F11), a bounded ~150-line rotator plus its tests is
the cheaper long-run commitment, and it lets us guarantee "a rotation failure
never drops a log line" — a property B does not offer. D cannot be correct on
Windows at all: Task Scheduler discards the stream, and a `cmd.exe` wrapper
would be killed by `schtasks /end` before the daemon. E is rejected because it
leaves the Windows user with no logs until they discover a flag, which is the
status quo with extra steps.

The cost is a small amount of rotation logic to own and test, and one new
concept (`log.file`) in two config surfaces. It is accepted.

### The decisions

**D1 — Each daemon owns its log file, on every platform.** `mcremote serve` and
`mcrelay serve` open, append to, rotate, and prune a log file themselves, and
close it on shutdown. Neither depends on systemd, launchd, or the Task Scheduler
to produce the file. The active file is opened for **append across restarts —
never truncated** — so restarting the daemon does not erase the record of why it
restarted. *(F1, F2, F3)*

**D2 — Tee, do not replace.** Every record is written to both the file and the
process's `stderr`, and the two writes are **failure-independent**: a failing
file must not suppress the stderr copy, and a failing stderr must not suppress
the file copy. `io.MultiWriter` does not provide this — it aborts at the first
failing writer — so the implementation is a small tee that ignores the stderr
result and returns only the file's. This matters most on Windows, where a
detached Task Scheduler daemon's `stderr` may have no reader at all.
journald/launchd capture, interactive `serve`, and certmagic's direct
`os.Stderr` output all survive; the file is additive. *(F6)*

**D3 — One unified default path, computed from `Roots` (amends MADR 0116 F4).**
`Resolve` builds `Paths.LogDir` via `roots.joinProduct(roots.Logs, product.Name)`,
and the per-platform `Logs` base becomes: Darwin `~/Library/Logs`, Linux
`$XDG_STATE_HOME` (`~/.local/state`), Windows `%LocalAppData%\<product>\Logs`
(scoped, unchanged). The daemon's active file is `<LogDir>/<product>.log`:

| Platform | `Paths.LogDir` | Active file |
| --- | --- | --- |
| Linux | `$XDG_STATE_HOME/mcremote` (`~/.local/state/mcremote`) | `…/mcremote/mcremote.log` |
| macOS | `~/Library/Logs/mcremote` | `…/mcremote/mcremote.log` |
| Windows | `%LocalAppData%\mcremote\Logs` | `…\mcremote\Logs\mcremote.log` |

This fixes F4 (the Windows double leaf), makes `docs/ops-windows-install.md:58`
and `acceptance-windows.ps1:142-143` true, and gives Linux the XDG-correct
location (XDG designates `XDG_STATE_HOME` for logs). On Linux this makes
`LogDir` and `StateDir` the same directory (`$XDG_STATE_HOME/<product>`); that
is accepted deliberately — the XDG root is correct, the prune scan is prefix-
and suffix-gated so it cannot touch state content, and a dedicated `logs/` leaf
would require platform-specific `Resolve` logic, which is exactly what D3
removes. A test pins the equality so it cannot drift unnoticed. *(F4, F5, F15,
F17)*

**D4 — Every knob is available as config, environment variable, and flag**, in
the same shape as `log.level`/`log.format` (mcremote `config.go:416-420`,
`load.go:45-46`, `root.go:88-89`; mcrelay `fileconfig.go:143-147`, `230-231`,
`519-520`, `cli.go:90-91`). The keys and their `MCREMOTE_` / `MCRELAY_`
counterparts:

| Config | Env | Flag |
| --- | --- | --- |
| `log.file` | `MCREMOTE_LOG_FILE` / `MCRELAY_LOG_FILE` | `--log-file` |
| `log.max_size_mb` | `MCREMOTE_LOG_MAX_SIZE_MB` / `MCRELAY_LOG_MAX_SIZE_MB` | `--log-max-size-mb` |
| `log.max_backups` | `MCREMOTE_LOG_MAX_BACKUPS` / `MCRELAY_LOG_MAX_BACKUPS` | `--log-max-backups` |
| `log.max_age_days` | `MCREMOTE_LOG_MAX_AGE_DAYS` / `MCRELAY_LOG_MAX_AGE_DAYS` | `--log-max-age-days` |
| `log.compress` | `MCREMOTE_LOG_COMPRESS` / `MCRELAY_LOG_COMPRESS` | `--log-compress` |

*(F11's spirit: no flag-only or env-only setting; the three surfaces cannot
drift.)* Validation moves with the keys: `Config.Validate` — today only
`log.level`/`log.format` (mcremote `internal/config/config.go:1119-1127`;
mcrelay `internal/relay/fileconfig.go:563-568`) — additionally rejects control
characters in `log.file` and negative rotation values, while accepting `0` as
the documented "unbounded" value from D5.

**D5 — Intelligent defaults that bound disk use.** `file: ""` (platform default
from D3), `max_size_mb: 10`, `max_backups: 5`, `max_age_days: 28`,
`compress: true`. The special value `log.file: "off"` disables the file sink;
`-` is deliberately *not* used, because it conventionally means stdout, and here
the alternative to a file is stderr. The active file plus five backups bound the
**uncompressed** total at about 60 MB (10 MB active + 5 × 10 MB); with
`compress: true` a text log lands far below that in practice, but the bound is
stated uncompressed because compression is not guaranteed and gzip adds
per-file overhead. `max_age_days` caps how long any backup survives regardless
of count. `0` means "no bound of that kind", so an operator can opt into
unbounded retention explicitly. *(F14)*

**D6 — Rotation semantics.** Check before each write; if the active file would
exceed `max_size_mb`, rotate first. On open, rotate if the existing active file
is already at or over the cap. A single record larger than the cap is written to
a fresh file rather than dropped. Rotated names are
`<base>-<UTC timestamp>.log` (millisecond precision), gaining `.gz` when
compressed. Because `os.Rename` onto an existing target **succeeds silently on
Windows** (`MOVEFILE_REPLACE_EXISTING`, F16), a name collision would destroy the
previous backup: the rotator therefore appends `-1`, `-2`, … before the
extension until the name is free, and prune's timestamp parser accepts the
suffix. Pruning by count and by age runs after each rotation. Writes and rotation are serialized by a
mutex. The active file is opened `0600` inside a directory converged with
`appdirs.EnsurePrivateDir`, so the file is private on both POSIX and Windows.
The name stays `<product>.log` — per-product, not per-`InstanceKey` — so two
daemons with different `--data-dir` share one active file; that is the existing
macOS behaviour (F10) and is documented, and instance-keyed names are deferred.
*(D3, F10)*

**D7 — Rotation never drops a log line.** If rotation, pruning, or compression
fails, the sink keeps appending to the current file and reports the failure once
on `stderr`; it does not return an error that would make `slog` discard the
record. This is the property option B does not provide. *(F12)*

**D8 — Explicit path failures are fatal; an implicit default failure is not.**
When the operator set `log.file`/`--log-file`, failing to open it is a startup
error. When the path is the platform default, failure logs a warning and the
daemon continues on `stderr` only — a missing log directory must not prevent the
service from running. *(F3)*

**D9 — The rotator is built in-tree, dependency-free, in `internal/logging`.**
No new module; `go.mod`/`go.sum` are untouched and CI's tidy gate stays green.
*(F11, F13)*

**D10 — `setup-service` reports the real log path on every platform.** `Setup`
sets `Result.LogDir` for all three OSes from the resolved paths; `result_print.go`
gains a `windows-task` case that prints the log path and a Windows tail command
(`Get-Content -Wait`), and keeps the launchd and systemd cases. The docs and the
printed guidance state the F16 caveat: after a rotation the tail is following
the renamed backup and must be re-attached to see new lines. The launchd
path's hardcoded `filepath.Join(home, "Library", "Logs", opts.Product)`
(`setup.go:431`) is replaced by the resolved `Paths.LogDir`, so the plist's
stdio directory and the daemon's own file cannot diverge. *(F7, F8)*

**D11 — mcrelay reaches parity.** The same keys, defaults, and sink; and
`mcrelay paths` emits `log_dir` in both JSON and text. *(F9)*

**D12 — Documentation, embedded defaults, and tests move with the code.**
`docs/config.md`, `docs/config-mcrelay.md`, `docs/ops-windows-install.md`, the
embedded `defaults_*.yaml`, and `examples.go` are updated; the rotator and the
platform `LogDir` gain unit tests; the Windows acceptance script asserts the
file exists and contains a startup line. *(F4, F5, F15)*

**D13 — `internal/logging` exposes a closable opener, not only a writer.**
Alongside the existing logger-only `Setup`, add an opener returning
`(*slog.Logger, io.Closer, error)`; `Setup` becomes a thin wrapper over it for
callers and tests that want only the logger. Both `serve` entry points use the
opener and `defer` the closer (with a flush), so the file is closed cleanly on
shutdown. This is the API consequence of D1's "close it on shutdown" and D2's
two-destination writer. *(D1, D2, D6)*

**D14 — `doctor` names the active log file.** `mcremote doctor` reports the
resolved log path, the active file's size, and whether it is writable — the
fastest answer to "where are the logs" on a host with no service-manager tail
command (Windows). This is a read-only addition to the existing `doctor`
output; it does not probe content. *(F7)*

### Consequences

* Good: Windows gets a log file at the documented path; Linux and macOS gain a
  daemon-owned file they no longer depend on the service manager for; disk use
  is bounded by default; `paths`, docs, and acceptance finally agree.
* Good: `--log-file` plus env/config gives operators the control the request
  asked for, and `--log-max-size-mb` etc. make the retention policy explicit.
* Bad: two overlapping files appear on macOS (`mcremote.log` alongside launchd's
  `.out.log`/`.err.log`) and a redundant channel on Linux (file plus journald).
  Accepted: the overlap is what keeps non-`slog` stderr reachable (F6), and the
  alternative — removing the platform capture — loses certmagic and panic
  output.
* Bad: a per-product (not per-instance) file means two daemons with different
  `--data-dir` share one active file. Accepted as the existing macOS behaviour
  (F10), and stated in the docs; a future record may key the filename by
  `InstanceKey`.
* Neutral: on Linux the log file lives inside `StateDir` (F17). Accepted: the
  XDG root is correct, prune cannot match state content, and the alternative
  (a `logs/` leaf) would reintroduce per-platform `Resolve` logic. Pinned by a
  test so the overlap stays a decision, not a surprise.
* Neutral: a Windows `Get-Content -Wait` tail follows the backup after a
  rotation (F16) and must be re-attached. Documented beside the tail command.
* Neutral: the rotator is ours to maintain. Bounded by a small, well-defined
  contract and a test matrix the plan pins.

### Confirmation

```text
# Path contract (fixes F4; must hold on all three OSes)
mcremote paths --json | jq -r .log_dir   → platform default from D3, no doubled product leaf
mcrelay  paths --json | jq -r .log_dir   → the same, for mcrelay
Linux only: .log_dir == .state_dir       → the F17 overlap, pinned not assumed

# Self-owned file exists and grows
mcremote serve --log-file <tmp>/m.log ... ; assert <tmp>/m.log contains the startup line

# Rotation and retention
serve with log.max_size_mb=1, log.max_backups=2, log.compress=true
  → active file rotates; ≤2 backups retained; backups end in .log.gz
  → observed active-plus-backups size recorded from the run (OQ2: a number,
    not the 60 MB bound)
  → a backup-name collision yields a -1/-2/… suffix; no backup is overwritten

# Rotation failure does not drop lines
make the directory read-only mid-run → the next record still appears on stderr
  and on the active file; one warning is emitted

# Rotation under a Windows tail (measured 3/3 on this host; acceptance re-pins)
Get-Content -Wait attached → os.Rename succeeds, daemon keeps writing the new
  active file, the tail follows the backup and is re-attachable

# Disable, close, and report
log.file: "off" (or --log-file off) → no file is created; stderr only
SIGTERM the daemon → the active file is closed and flushable by the OS
mcremote doctor → names the resolved log path, its size, and writability

# No regression on the platform channels
journalctl --user -u mcremote -f   → still receives lines (Linux)
launchctl print ... ; cat ~/Library/Logs/mcremote/mcremote.err.log → still receive lines (macOS)

# Whole-tree gates
make test && make race && make lint && make tidy && make preflight

# Windows
make ci-windows ; scripts/acceptance-windows.ps1   → both pass, including paths --json (C1)
mcremote setup-service --force
  → prints the resolved log path and a Get-Content -Wait command
start the task; assert %LocalAppData%\mcremote\Logs\mcremote.log contains the startup line
```

## Pros and Cons of the Options

### A — In-tree rolling file sink, teed to `stderr` (chosen)

* Good, because it needs no service manager to create a file and therefore
  works identically on Windows, Linux, and macOS (F2).
* Good, because it lets rotation be best-effort: a failed rename or prune never
  discards a record (D7), which the library options cannot promise (F12).
* Good, because it keeps the dependency surface flat and CI's tidy/vuln gates
  untouched (F11, F13).
* Good, because teeing preserves every existing capture path and the
  non-`slog` output the file would otherwise miss (F6).
* Bad, because rotation is subtle; concurrency, rename-over-open semantics on
  Windows, and retention need their own test matrix.
* Bad, because it is code the project maintains forever, where a library would
  absorb upstream fixes.

### B — Adopt `gopkg.in/natefinch/lumberjack.v2`

* Good, because it is battle-tested, MIT-licensed, transitively stdlib-only,
  and imported very widely (`v2.2.1`, 9,058 importers).
* Bad, because it is size-only and feature-frozen (last release 2023-02-06;
  `v3` remains alpha), so "intelligent defaults" beyond size would still be
  ours to build.
* Bad, because its documented one-process-per-file constraint and its
  `Write`-returns-error behavior on a failed rotate — an error `slog` ignores,
  silently losing the line (F12) — leave exactly the two problems (F10, D7)
  that need the most care.
* Bad, because it would still need the private-directory, tee, and Windows
  rename work wrapped around it, so it removes less than the full surface.

### C — Adopt `github.com/DeRuina/timberjack`

* Good, because it adds time- and clock-based rotation plus gzip/zstd, which is
  closer to "intelligent" out of the box.
* Bad, because it is young (created 2025-04-28, 155 stars, one maintainer) and
  a rotation bug is silent data loss; its API churn is a live risk.
* Bad, because the retention half of "intelligent" (size, count, age,
  compression) is not the part that benefits from a fork of lumberjack; the
  clock-scheduling half is a convenience, not a requirement.

### D — Service-manager / stream redirection only

* Good, because it changes no Go code and no config surface.
* Bad, because it cannot work on Windows: Task Scheduler discards the stream,
  and a `cmd.exe` wrapper is killed by `schtasks /end` before the daemon
  (F2).
* Bad, because it keeps the daemon dependent on systemd/launchd, which is
  exactly what the request rejects.
* Bad, because it does not solve rotation or retention at all.

### E — Explicit-only file sink (no default)

* Good, because it never surprises an operator with a new file.
* Bad, because the default experience on Windows remains "no logs", so the bug
  is only fixed for users who already know to pass a flag.
* Bad, because it leaves `Paths.LogDir` with no default consumer, and the
  docs/acceptance path promise (F15) still ungratified.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Sink defaults to `os.Stderr`; no file producer exists | `internal/logging/slog.go:15`, `20-24`; tree-wide search for `lumberjack`/`rotat`/`OpenFile`/`log_file` |
| Both daemons call `logging.Setup` with no `Out` | `internal/cli/serve.go:80-83`; `internal/relay/cli.go:257-260` |
| Windows task has no output redirection | `internal/cli/service/schtasks.go:126-129`; `setup_schtasks.go:32-49` |
| Windows resolved `log_dir` has a doubled leaf | `go run ./cmd/mcremote paths --json` on this host; `internal/appdirs/paths.go:70` vs `roots.go:27-35`, `roots_windows.go:64,67` |
| Docs and acceptance assert `%LocalAppData%\mcremote\Logs` | `docs/ops-windows-install.md:58`; `scripts/acceptance-windows.ps1:61-67`, `142-144` (`git blame` `ca436bbc`, 2026-09-06) |
| Linux has no `Logs` root, pinned by a test | `internal/appdirs/roots_unix.go:48-54`; `internal/appdirs/systempaths_test.go:38-41` |
| Only `DataDir`/`ConfigDir` are converged | `internal/daemon/daemon.go:86`, `100-106` |
| launchd path creates its log dir; its `Result.LogDir` comment says macOS-only | `internal/cli/service/setup.go:431-435`, `112-113` |
| certmagic logs directly to `os.Stderr` | `internal/certs/acme.go:191-203` |
| `slog` logger methods ignore a handler's write error, so a writer that errors loses the record | Go 1.26.6 `src/log/slog/handler.go:322` returns the `Write` error; `Logger.Info`/`Warn`/… do not observe it. Re-verified against the locally installed `go1.26.6 windows/amd64` GOROOT source (`commonHandler.handle`: `_, err := h.w.Write(*state.buf); return err`) |
| Windows rename-while-tailed succeeds; rename replaces existing targets; open backups are removable; the tail follows the handle | Host probe 2026-09-13, go1.26.6 windows/amd64, 3/3 identical runs: `os.Rename` under a live `Get-Content -Wait` reader, recreate+append, second rotation, rename-over-existing, `os.Remove` of the held backup — all `err=<nil>` |
| Linux `LogDir` would equal `StateDir` under the D3 `Logs` base | `internal/appdirs/paths.go:59` (`StateDir = joinProduct(StateHome, name)`), `roots_unix.go:44` (`StateHome = $XDG_STATE_HOME`) |
| `BindPFlag` is `Changed`-gated, so zero-value flag defaults cannot shadow `SetDefault` | `internal/config/load.go:435`, `internal/relay/fileconfig.go:545`; viper `find()` prefers a pflag only when `HasChanged()`, else falls back to env/config/defaults |
| Startup log lines the acceptance script asserts | `internal/cli/serve.go:102` (`"starting mcremote"`), `internal/relay/cli.go:292` (`"mcrelay starting"`) |
| Test seams the plan relies on exist | `OverrideInstallOS` (`internal/cli/service/setup.go:202`), `resolveProductPaths` (`setup.go:1030`), `installOS` dispatch (`setup.go:301-315`) |
| `make ci-windows` / `ci-windows-smoke` targets | `GNUmakefile` + `make/ci-windows.mk` (MADR/PLAN 0145); they run `scripts/ci-windows-local.ps1` |
| `result_print.go` has no `windows-task` case | `internal/cli/service/result_print.go:44-56`, `118-135` |
| mcrelay `paths` omits `log_dir` | `internal/relay/cli.go:126-138`, `140-150` |
| Log path is per-product, not per-instance | `internal/appdirs/paths.go:70`, `96-99`; `internal/cli/service/plist_render.go:56-61` |
| Project preference for dependency-free narrow code | MADR 0024 (dependency-free package); PLAN 0028 ("do not add … for JSON-RPC"); PLAN 0065 ("stdlib only; no new deps"); PLAN 0029 (deps map to boundaries) |
| lumberjack v2.2.1, MIT, stdlib-only, size-only, one-process, 2023-02-06 | pkg.go.dev `gopkg.in/natefinch/lumberjack.v2`; source `raw.githubusercontent.com/natefinch/lumberjack/v2.2.1/lumberjack.go` (`:19-21`, `:79-116`, `:139-144`) |
| timberjack created 2025-04-28, MIT, 155 stars | github.com/DeRuina/timberjack |
| CI enforces gofmt, tidy, vet, race | `.github/workflows/ci.yml:84-92`, `94-104`, `106-115` |
| House style of an explicit, reasoned budget | `internal/cli/serve.go:143-165` (`defaultMemoryLimit`) |
| Linux/macOS capture via unit/plist | `internal/cli/service/mcremote.user.service.tmpl:54-57`; `internal/cli/service/plist_render.go:177-178` |

### Related records

* [0116-MADR-windows-and-linux-arm64-build-targets.md](0116-MADR-windows-and-linux-arm64-build-targets.md)
  — defines the Windows Known-Folder layout (D3), the product-scoped `Roots`
  that causes F4, and the deliberate absence of a Linux log root (F4/F5 here).
  This record amends its F4 and D3 log-path handling.
* [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md)
  — D6 established launchd `StandardOutPath`/`StandardErrorPath` under
  `~/Library/Logs/<product>`; this record keeps those and adds a daemon-owned
  file beside them.
* [0059-MADR-native-paths-and-linux-macos-parity.md](0059-MADR-native-paths-and-linux-macos-parity.md)
  — scopes `Logs` to agent stdio on Darwin; this record widens it to a
  daemon-owned file on all platforms.
* [0024-MADR-stream-coalescing.md](0024-MADR-stream-coalescing.md),
  [0028-PLAN-codex-provider.md](0028-PLAN-codex-provider.md),
  [0029-PLAN-provider-platform-canonicalization.md](0029-PLAN-provider-platform-canonicalization.md),
  [0065-PLAN-update-automation.md](0065-PLAN-update-automation.md)
  — the dependency-aversion precedent (F11).
* [0145-MADR-local-windows-ci-style-tests.md](0145-MADR-local-windows-ci-style-tests.md)
  — `scripts/acceptance-windows.ps1`, whose `paths --json (C1)` check this
  record repairs (F4).

### Open questions for the plan

1. **Windows rename-while-tailed behaviour — CLOSED (probed 2026-09-13).** Was
   [unverified]; the host probe (3/3 identical runs, go1.26.6 windows/amd64)
   measured rename-under-tail success, rename-replaces-existing, and removable
   open backups (F16). D7's fallback stays as the portable behaviour for a
   genuinely failed rename (e.g. a reader that opens without `FILE_SHARE_DELETE`,
   or a future Windows change); the happy path is now the *expected* path, and
   PLAN P7 re-pins it on the acceptance host rather than discovering it.
2. **Steady-state size is a bound, not a measurement — assigned to PLAN P7.**
   D5 states the uncompressed ceiling; P7's rotation run records the observed
   active-plus-backups size so "intelligent" is a number, not a claim.
3. **Manual rotation — CLOSED as deferred.** Rotation-on-size bounds the file
   for this release; SIGHUP/console-signal rotation is named in the PLAN's
   Deferred section (Windows has no SIGHUP, so it needs its own record).
