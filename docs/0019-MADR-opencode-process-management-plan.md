# MADR 0019: OpenCode process management — remove the ACP transport, guarantee a single engine

- **Status**: Accepted — implemented 2026-07-24 on `feat/opencode-single-engine`
  (7 commits; §10 records what implementation changed relative to this plan)
- **Date**: 2026-07-24
- **Related**: [MADR 0011](./0011-opencode-provider-plan.md) (OpenCode provider; introduced both
  transports), [MADR 0004](./0004-phase2-grok-acp.md) (grok ACP — unaffected),
  [MADR 0012](./0012-mcremote-daemon-assessment-action-plan.md) (daemon assessment),
  [MADR 0020](./0020-opencode-session-tree.md) (HTTP dialect session tree + async
  control plane — **Accepted**, follows this MADR’s single-engine invariant)

---

## 1. Problem

Two defects, one structural and one operational.

**Operational — engines outlive their daemon.** Observed on the dev host on 2026-07-24:

```
62494   PPID 1        19h05m   210 MB   opencode serve --hostname 127.0.0.1 --port 46001   ← orphan, still LISTENING
103412  PPID 103387   13h02m   336 MB   opencode serve --hostname 127.0.0.1 --port 40099   ← current, correct
```

The orphan is a leaked engine from a previous `mcremote` run: reparented to init, holding a
port and 210 MB, with no daemon and no sessions. Engine teardown exists **only** on the happy
path — a `defer` in `daemon.go:165-171` calling `httpagent.Provider.Shutdown` (`provider.go:130`).
Any abnormal exit (`kill -9`, panic, dev-loop rebuild, crash-then-`Restart=always`) skips it.
`procutil.SetProcessGroup` puts the engine in its *own* process group, which actively shields it
from the terminal's Ctrl-C/SIGHUP. There is no `Pdeathsig`. Every such exit leaks one engine
**permanently**.

**Structural — a legacy transport doubles the process-management surface.**
`providers.opencode.transport` selects between:

- `http` (default, and what is deployed) — one shared `opencode serve` engine, sessions are cheap
  server-side objects, `Close` tears down local state only.
- `acp` (legacy) — one `opencode acp` subprocess **per session** plus a warm spare, with no idle
  reaping anywhere in the manager (`autoClose` fires only on process death,
  `manager.go:430-433`), so abandoned sessions pin full Bun engines up to
  `limits.max_live_sessions` (16).

Keeping `acp` means every process-management guarantee has to be stated and tested twice, and the
"one engine per host" invariant becomes conditional. Removing it makes the invariant structural.

---

## 2. Decision

1. **Delete the OpenCode↔ACP binding.** `internal/provider/acpagent/` **stays** — grok depends on
   it (`grok/grok.go:48,53`) and keeps it compiled and exercised. Only the ~500-line OpenCode
   binding, its four dead/legacy config keys, and its docs go.
2. **Make the engine strictly daemon-owned.** No `opencode serve` process may outlive the
   `mcremote` that spawned it. Enforced by four independent layers (§5.2), not one.
3. **Reject adoption of a pre-existing engine** (alternative considered and declined, §6.1).

Resulting invariant, single sentence, mechanically checkable:

> **Each running `mcremote` owns exactly one `opencode serve` process, and no `opencode serve`
> process spawned by `mcremote` survives its owner.**

---

## 3. Blast radius

Everything below was verified by inspection on `master` @ `730b0fe`. Line numbers are current.

### 3.1 Go — delete

| Path | Detail |
|---|---|
| `internal/provider/opencode/opencode.go` | 116 lines. The ACP `Spec` (`:46`), `Provider`/`New`/`NewWithLogger` aliases (`:57-67`), `configureSession` (`:76`), `modelConfigID` (`:70`). **Exception:** `staticModelOptions()` (`:27`) is still called by `http.go:94` — move it into `http.go`, delete the file. |
| `internal/provider/opencode/opencode_test.go` | 6 tests, all ACP-only: `TestNewDefaults`, `TestDefaultArgs`, `TestConfigureSession{NoModelIsNoop,ModelAlreadyCurrent,MissingModelOptionIsSoft}`, + the config-default-model case at `:63`. |
| `internal/provider/opencode/live_test.go` | 8 live ACP tests under `//go:build live_opencode`. **See §3.6 — port first.** |

### 3.2 Go — edit

| Path:line | Change |
|---|---|
| `internal/daemon/daemon.go:138-161` | Collapse the `if Transport == "acp" { … } else { … }` to the HTTP path only. Drop the `opencode.NewWithLogger` + `EnsureWarm()` branch. |
| `internal/daemon/daemon.go:160` | Gate `EnsureServer()` on `cfg.Providers.Opencode.Prewarm` (§4.2). |
| `internal/provider/opencode/http.go` | Absorb `staticModelOptions()`; update the package doc comment (currently split across two files, both claiming to be the package doc). |
| `internal/provider/opencode/http_delta_test.go:243` | Uses `zenFallbackModels` — unaffected, verify it still compiles after the move. |
| `internal/provider/httpagent/httpagent.go:32-35` | `Config` alias comment says "Args and Prewarm are ACP-specific and ignored here" — `Prewarm` becomes honoured (§4.2); `Args` genuinely goes. |
| `internal/provider/provider.go:24` | Comment: "the OpenCode provider (HTTP or ACP transport)" → HTTP only. |
| `internal/protocol/messages.go:95` | Comment: "opencode sets the ACP \"model\" session config option" → describes the REST create/prompt bodies now. |
| `internal/ws/server.go:551` | Comment: "opencode = a full Bun engine per process" — no longer true under the shared engine. |

### 3.3 Config — schema

`internal/config/config.go`, `OpencodeProviderConfig` (`:362-400`):

| Key | Disposition | Evidence it is already dead |
|---|---|---|
| `transport` (`:371`) | **Delete** + migration guard (§4.1) | the switch itself |
| `args` (`:372`) | **Delete** | `config.go` comment + all three shipped configs: "acp transport only; the http engine's argv is fixed" |
| `fs_roots` (`:393-398`) | **Delete** | `config.go:393`: "effective only on the `acp` transport — the HTTP engine does its own file I/O" |
| `prewarm` (`:383-387`) | **Keep, make real** (§4.2) | `httpagent.go:34`: "Args and Prewarm are ACP-specific and **ignored here**" — the key is silently inert today |
| `enabled`, `bin`, `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `turn_stall_notice_seconds` | unchanged | — |

Also:

- `internal/config/config.go:439-446` — drop `Transport: "http"` from `Defaults()`.
- `internal/config/config.go:596-600` — replace the `""|http|acp` switch with the migration guard.
- `internal/config/load.go:154` — drop `v.SetDefault("providers.opencode.transport", …)`.
- `internal/config/load.go:96` uses `v.Unmarshal`, **not** `UnmarshalExact` → unknown keys are
  silently ignored. Without an explicit guard, a deployed `transport: acp` would silently become
  HTTP. This is why §4.1 is not optional.
- Env override `MCREMOTE_PROVIDERS_OPENCODE_TRANSPORT` (and `…_ARGS`, `…_FS_ROOTS`) stop binding.
  The guard must check the *resolved* value so an env-set transport is caught too.
- `internal/config/config_test.go:54` touches `cfg.Providers.Opencode`; `:446` sets
  `TurnStallNoticeSeconds` — neither references the removed keys, but add guard coverage (§5.4).

### 3.4 Config — shipped files

| Path:line | Change |
|---|---|
| `internal/cli/service/defaults_mcremote.yaml:53-64` | **`//go:embed`-ed** (`setup.go:26`) — this is what `setup-service` writes for a *new* install, so it is the highest-impact of the four. Drop `transport`, `args`, `fs_roots` |
| `configs/config.example.yaml:99-112` | drop `transport`, `args`, `fs_roots`; rewrite the `prewarm` comment |
| `configs/config.mesh-grok.yaml:110-127` | same; **`:121` currently states "HTTP transport: prewarm boots the shared `opencode serve` at daemon start" — false today**, true after §4.2 |
| `configs/config.prod.example.yaml:85-93` | drop `transport` (`:85`), `args` (`:86`), `fs_roots` (`:93`) |
| `~/.config/mcremote/config.yaml` (live, not in repo) | operator task — §7.3 |

> Note: an earlier revision of this MADR listed only the three `configs/*.yaml`
> files. `internal/cli/service/defaults_mcremote.yaml` is embedded into the
> binary and is what every fresh `setup-service` install receives — omitting it
> would have shipped the retired keys to new users on day one.

### 3.5 Docs

| Path:line | Change |
|---|---|
| `README.md:322` | config-key table row for `providers.opencode` — remove `transport`, `args`, `fs_roots` |
| `docs/config.md:70` | delete the `transport` row |
| `docs/config.md:71` | delete the `args` row |
| `docs/config.md:76` | rewrite `prewarm` row (drop the "acp: keep one spare" half) |
| `docs/config.md:78` | delete the `fs_roots` row |
| `docs/0011-MADR-opencode-provider-plan.md` | **Do not rewrite history.** Append a status banner pointing at this MADR: ACP chosen in 0011, superseded here. Lines `:31-35`, `:44-49`, `:226-227` are the relevant claims. |
| `docs/0019-…` (this file) | mark Accepted on merge |

### 3.6 Test coverage — the one genuine regression

`live_test.go` carries **8** live ACP tests; `live_http_test.go` carries **2**
(`TestLiveHTTPPromptStream`, `TestLiveHTTPResume`). Deleting ACP deletes the better-covered path.
These six behaviours have **no HTTP equivalent** and must be ported before the delete lands:

| ACP test | HTTP port |
|---|---|
| `TestLiveOpencodePermissionRoundTrip` | permission request → phone answer → resolve, over SSE |
| `TestLiveOpencodeCancel` | `Cancel` → `ds.Abort` → turn ends |
| `TestLiveOpencodeConcurrentSessions` | N sessions, one engine, no event cross-talk — **now also asserts one process** |
| `TestLiveOpencodeInvalidModel` | bad model id surfaces as a clean error |
| `TestLiveOpencodeModelSelection` | per-session model override wins over config default |
| `TestLiveOpencodePrewarmFastCreate` | becomes "create against a pre-warmed engine is < 2s" |

`TestLiveOpencodePrompt` / `TestLiveOpencodeResume` are already covered by the two HTTP tests.

### 3.7 Confirmed **not** affected

- **Mobile app** — no transport awareness. `mcremote_client.dart:1405` enumerates provider ids
  (`grok`, `opencode`, `fake`); `chat_screen.dart:102` displays the id. No Dart changes.
- **CI** — `.github/workflows/ci.yml` runs no live-tagged tests and never references opencode.
- **Makefile** — no `live`/`opencode`/`acp` targets.
- **`scripts/`** — no references to opencode.
- **Wire protocol** — `internal/protocol/messages.go` carries no transport field; comment only.
- **`internal/provider/acpagent/`** (2090 lines) and **`internal/provider/grok/`** — untouched.
- **`internal/picker`, `internal/agenterr`, `internal/session`** — provider-agnostic.

---

## 4. Config changes in detail

### 4.1 `transport` removal guard (one release)

Because viper is lenient, silence is the dangerous outcome. In `Config.validate()`, replacing the
current switch:

```go
// providers.opencode.transport was removed in 0019: OpenCode is always driven
// through the shared `opencode serve` engine. Fail loudly rather than silently
// changing behaviour for a config that still pins the retired ACP transport.
if c.Providers.Opencode.TransportRemoved != "" {
    return fmt.Errorf(
        "providers.opencode.transport is no longer supported (was %q); OpenCode "+
            "now always uses the shared HTTP engine — remove the key from your config",
        c.Providers.Opencode.TransportRemoved)
}
```

Keep a `TransportRemoved string \`mapstructure:"transport"\`` shim field for exactly one release so
the error can name the offending value, then delete the field. Same treatment is **not** needed for
`args`/`fs_roots`: both were already inert on `http`, so ignoring them changes nothing.

### 4.2 Make `prewarm` honest

Today `providers.opencode.prewarm` is documented in three shipped configs, defaults to `true`, and
does **nothing** — `EnsureServer()` at `daemon.go:160` runs unconditionally. Two options:

- **(a) Delete the key.** Fewest knobs. But anyone who set `prewarm: false` expecting reduced memory
  gets a silent behaviour change in the direction they didn't want.
- **(b) Honour it** — `if cfg.Providers.Opencode.Prewarm { op.EnsureServer() }`. Three lines. Turns a
  documented lie into truth, and gives memory-tight hosts a real lazy-boot option (the engine is
  210–336 MB resident). First session then pays the ~3–5 s cold start.

**Recommend (b).** It is strictly less work than deleting the key from three configs plus two doc
tables, and it removes a false statement rather than a true one.

### 4.3 Final `providers.opencode` shape

```yaml
opencode:
  enabled: true
  bin: "opencode"
  always_approve: false
  default_cwd: ""
  model: "opencode/deepseek-v4-flash-free"
  permission_timeout_seconds: 120
  # Boot the shared `opencode serve` engine at daemon start so the first
  # session create is instant. false = boot lazily on first use (~3-5s),
  # saving ~250MB while idle.
  prewarm: true
  turn_stall_notice_seconds: 120
```

11 keys → 8.

---

## 5. Process-management hardening

All of it lands in `internal/provider/httpagent/provider.go` + `internal/procutil/`.

### 5.1 Engine ownership marker

Spawn the engine (`provider.go:183`) with two extra env vars:

```
MCREMOTE_ENGINE_ID=<uuid>                 # this engine instance
MCREMOTE_ENGINE_OWNER=<pid>:<starttime>   # the owning mcremote
```

`starttime` is field 22 of `/proc/<pid>/stat` (clock ticks since boot). Pairing it with the pid
makes the owner reference immune to PID reuse — the reuse window is exactly what makes a naive
`kill(pid,0)` check unsafe.

This marker is the **only** thing that authorises a kill. We never fingerprint by argv: a human's
hand-started `opencode serve --hostname 127.0.0.1 --port N` is indistinguishable from ours by
command line, and killing it would be unacceptable. `/proc/<pid>/environ` is readable only for
same-uid processes, which is precisely the set we can have spawned.

### 5.2 Four layers, defence in depth

| # | Layer | Covers | Notes |
|---|---|---|---|
| 1 | `Pdeathsig: SIGKILL` (Linux) | `kill -9` of mcremote, panic, dev-loop restart | Best-effort: Go's runtime can migrate goroutines between threads and `Pdeathsig` keys on the *forking thread's* death. Mitigate with `runtime.LockOSThread` around `cmd.Start()`. Fires on the direct child only, not its group — acceptable: the engine was observed to have no children. |
| 2 | Graceful `Shutdown()` | clean SIGTERM (systemd stop/restart, Ctrl-C) | §5.3 |
| 3 | Boot-time orphan sweep | anything layers 1–2 missed | §5.4 |
| 4 | `KillMode=control-group` | systemd-managed hosts | §7.1 |

No single layer is trusted. Layer 3 is the backstop that makes the invariant hold even if 1 and 2
both fail, which is exactly what happened to produce pid 62494.

### 5.3 Graceful stop

`procutil.KillProcessGroup` sends **SIGKILL** only — even the clean shutdown path denies the engine
any chance to flush its session storage. Add:

```go
// TerminateProcessGroup sends SIGTERM to p's group, waits up to timeout for it
// to exit, then SIGKILLs. Returns whether the graceful term succeeded.
func TerminateProcessGroup(p *os.Process, done <-chan error, timeout time.Duration) bool
```

`Shutdown()` (`provider.go:130`) then uses it. This requires the Provider to retain the `waitCh`
created at `provider.go:205` — currently it is captured only by the health poll and death monitor.
Store it alongside `cmd` (see §5.5).

Keep `KillProcessGroup` for the hard paths (`provider.go:212`, `:254`, sweep escalation).

### 5.4 Boot-time orphan sweep

Runs once, from `daemon.go`, before `EnsureServer()`:

1. Enumerate `/proc/[0-9]+/environ`, select processes carrying `MCREMOTE_ENGINE_ID=`.
2. For each, parse `MCREMOTE_ENGINE_OWNER=<pid>:<starttime>`.
3. **Reap only if the owner is gone** — owner pid absent, or present with a different `starttime`.
   An engine whose owner is alive belongs to a concurrently running `mcremote` (dev instance
   alongside the systemd unit) and must be left strictly alone.
4. Reap = `TerminateProcessGroup(…, 5s)`, log at INFO with pid/port/age.

Non-Linux (`procutil_other.go`): sweep is a no-op; layers 1–2 and the manual `mcremote engines`
verb carry it. Document the gap rather than pretending coverage.

### 5.5 Refactor `Provider` engine state

`Provider` currently holds `cmd *exec.Cmd` + `baseURL` + `generation` as three loose fields
(`provider.go:34-43`), with `waitCh` reachable only from closures. Introduce:

```go
type engine struct {
    cmd     *exec.Cmd
    waitCh  <-chan error
    url     string
    port    int
    id      string // MCREMOTE_ENGINE_ID
}
```

`Provider.eng *engine` replaces `cmd`/`baseURL`. Mechanical, but it is what makes §5.3 possible
without threading a channel through three call sites, and it keeps the `generation` fencing
(`:259`, `:281`, `:315`, `:375`) intact and readable.

### 5.6 `mcremote engines` admin verb

`internal/admin/` already carries a local unix socket (`<data_dir>/admin.sock`) used by
`mcremote pair revoke`. Add:

- `mcremote engines` — list marked engines: pid, port, owner pid, alive/orphan, RSS, uptime.
- `mcremote engines --reap` — run the §5.4 sweep on demand.

This is the operator escape hatch for the non-Linux gap and for "something is wrong right now".

### 5.7 Tests

**Unit** (`internal/provider/httpagent/provider_test.go` — currently one test):

- adoption is *not* attempted (regression guard against re-adding §6.1)
- concurrent `ensureServer` → exactly one spawn (exercises the `starting` gate, `:156-167`)
- `Shutdown` sends SIGTERM before SIGKILL, and SIGKILLs after the timeout
- `Shutdown` during the health poll kills the freshly-healthy process (`:249-257` — existing
  behaviour, currently untested)

**Unit** (`internal/procutil/procutil_test.go` — currently one test):

- `TerminateProcessGroup` graceful path (child traps SIGTERM, exits) and escalation path
  (child ignores SIGTERM → SIGKILL after timeout)
- owner-liveness parsing: dead owner → reap; live owner → skip; **reused pid with different
  starttime → reap**; unmarked process → never touched

**Integration** (new, `internal/daemon/`): fake engine binary in `testdata` that serves
`/global/health`, marked with the env vars; assert the sweep reaps an owner-dead instance and
leaves an owner-alive one running.

**Live** (`live_opencode` tag): the six ports from §3.6, plus a new
`TestLiveHTTPSingleEngineInvariant` — create 3 concurrent sessions, assert exactly one
`opencode serve` carries our `MCREMOTE_ENGINE_ID`.

---

## 6. Alternatives considered

### 6.1 Adopt a pre-existing engine instead of killing it — **declined**

A record file (`<data_dir>/opencode-engine.json`) plus a validation ladder (pid alive → cmdline
match → `/global/health` 200) would let a restarting daemon reuse the running engine, making
restarts instant.

Declined because it **directly conflicts with layer 1**: if `Pdeathsig` works, the engine is
already dead by the time the next daemon boots, so the adoption path would be dead code in the
common case and live only when the guarantee had failed. Choosing adoption means deliberately
*not* killing engines on daemon death, which weakens the invariant from "no engine outlives its
owner" to "duplicate engines get cleaned up eventually" — the weaker property is the one that
produced the 19-hour orphan.

It also costs a file-locking scheme (`internal/certs/filelock_unix.go` pattern), a boot-id check
for PID reuse across reboots, and a second death-monitor implementation (an adopted engine is not
a child, so `cmd.Wait()` is unavailable — it would need health polling).

The benefit it buys is a ~3–5 s engine boot on daemon restart, paid **in the background** by
`EnsureServer()`, which blocks nothing. Not worth the complexity or the weaker invariant.

*(This reverses the recommendation in the initial assessment, which proposed adoption before the
`Pdeathsig` interaction was worked through.)*

### 6.2 Keep `acp` as a fallback if OpenCode's HTTP surface breaks — **declined**

HTTP + SSE is the surface OpenCode's own clients use, so ACP is the likelier one to bit-rot.
MADR 0011 already records ACP-mode problems: a full engine per process (`:188`), SQLite contention
(upstream #15188, closed "not planned", `:193`), and `/undo`+`/redo` unsupported (`:180`) — HTTP is
a superset, not a subset. `acpagent` stays alive and tested via grok, so restoring an OpenCode ACP
`Spec` is ~50 lines recoverable from git history.

### 6.3 Idle-timeout the engine — **deferred**

`engine_idle_timeout_seconds` (stop the engine after N minutes with zero registered sessions) is
coherent but defeats pre-warm, which is the feature being asked for. `prewarm: false` (§4.2)
already serves the memory-constrained case. Revisit only if idle RSS growth (210 MB → 336 MB over
13 h, observed) proves to be an unbounded leak rather than steady-state caching.

---

## 7. Environment & operations tasks

### 7.1 systemd units

All three carry `KillMode=mixed` — SIGTERM to the main process only, SIGKILL to the cgroup
remainder after `TimeoutStopSec=45`. The engine therefore never receives a graceful signal even on
a clean `systemctl stop`. Change to `KillMode=control-group` in:

- `deploy/systemd/mcremote.service:26`
- `deploy/systemd/mcremote.user.service:43`
- `internal/cli/service/mcremote.user.service.tmpl:28`

`mcrelay.user.service:46` spawns no children — leave it.

Note: users with an already-installed unit must re-run `mcremote setup-service --force`; the
template change alone does not touch `~/.config/systemd/user/mcremote.service`. Call this out in
the release notes.

### 7.2 Immediate cleanup on the dev host

```
kill -- -62494      # reclaim 210 MB and port 46001
```

### 7.3 Live config migration

`~/.config/mcremote/config.yaml` currently sets `transport: "http"` (`:~99`), `args: []`,
`fs_roots: []`. After §4.1 lands, the daemon **refuses to start** until `transport` is removed.
This is intentional — but it means the config edit and the binary upgrade must be sequenced, or
`systemctl --user restart mcremote` fails with `Restart=always` retry-looping against
`StartLimitBurst=5`. Sequence: edit config → install binary → restart.

### 7.4 Verification after deploy

```bash
pgrep -af 'opencode serve' | wc -l        # expect exactly 1
mcremote engines                          # expect 1 row, owner = live mcremote pid
systemctl --user restart mcremote
sleep 8; pgrep -af 'opencode serve' | wc -l   # still exactly 1 — the old one died
kill -9 $(pgrep -f 'mcremote serve'); sleep 2
pgrep -af 'opencode serve' | wc -l        # 0 (layer 1) — or 1, reaped on next boot (layer 3)
```

---

## 8. Implementation plan

Six commits, ordered so nothing is built on code that is about to be deleted.

| # | Commit | Scope | Risk |
|---|---|---|---|
| 1 | `test(opencode): port ACP live coverage to the HTTP transport` | §3.6 — six new tests in `live_http_test.go`. **Land and run green against a real `opencode` before anything is deleted.** | none (additive) |
| 2 | `refactor(opencode): remove the ACP transport` | §3.1 delete, §3.2 edits, §3.3/§3.4 config, §4.1 guard, §4.2 prewarm, §3.5 docs | medium — config-breaking, see §7.3 |
| 3 | `refactor(httpagent): consolidate engine state behind one struct` | §5.5 — pure refactor, no behaviour change | low |
| 4 | `feat(procutil): graceful process-group termination` | §5.3 + its unit tests | low |
| 5 | `feat(httpagent): mark engines with ownership and die with the daemon` | §5.1 + §5.2 layers 1–2 + `Shutdown` using `TerminateProcessGroup` | medium — touches spawn path |
| 6 | `feat(daemon): reap orphaned opencode engines at startup` | §5.4 sweep, §5.6 `mcremote engines`, §5.7 integration tests, §7.1 systemd units | medium — must not kill a live daemon's engine |

Commits 3–6 are independent of 1–2 and could land first if the live-test port stalls; the ordering
above is preferred because commit 2 shrinks the surface that 3–6 must reason about.

### Definition of done

- [ ] `rg -i 'transport.*acp|acp.*transport' -- internal/ configs/ docs/config.md README.md` returns
      only the migration guard and this MADR
- [ ] `go build ./... && go test ./...` green
- [ ] `go test -tags live_opencode ./internal/provider/opencode/ -count=1 -timeout 300s` green
- [ ] `go vet ./...` and the repo's lint pass clean
- [ ] §7.4 verification script passes on the dev host, including the `kill -9` case
- [ ] `providers.opencode` documents 8 keys in `docs/config.md`, `README.md:322`, and all three
      `configs/*.yaml`, with no key that the code ignores
- [ ] MADR 0011 carries a superseded-by-0019 banner; this MADR is marked Accepted

---

## 9. Consequences

**Positive.** The one-engine-per-host invariant becomes structural rather than policy-enforced.
~500 lines of Go and 3 config keys are deleted; a fourth (`prewarm`) stops lying. The idle-session
reaping problem (defect D5 of the original assessment) disappears entirely — with no per-session
processes, there is nothing to reap. Orphan leakage is closed by four independent layers, and the
`kill -9` path — the one that actually produced the observed orphan — is covered by two of them.

**Negative.** OpenCode loses `fs_roots` (advisory-only by its own docstring, and already inert on
the HTTP transport). No ACP fallback if OpenCode's HTTP API breaks — mitigated by git history and
by `acpagent` staying alive under grok. Existing configs pinning `transport` must be edited before
upgrade (§7.3), and the sweep has no coverage on non-Linux hosts (§5.4).

**Neutral.** `internal/provider/acpagent/` (2090 lines) remains in the tree for grok. If grok is
ever retired, that package and `internal/provider/httpagent`'s `Config` alias to it become the next
simplification.

---

## 10. Implementation notes (2026-07-24)

Seven commits landed rather than six. What differed from the plan:

### 10.1 Defects found during implementation

**A shutdown arriving inside the engine's boot window leaked the engine.**
Not anticipated by this plan, and predating the whole series. `startServer`
publishes the engine only after the health poll succeeds (~3–5s), so a SIGTERM
before that left `Shutdown` looking at a nil engine and signalling nothing —
while a live `opencode serve` had already been spawned. `startServer` does
re-check `p.closed` and stop its own engine, but only while the daemon is alive
to run that code, and it had already exited.

Fixed in commit 7: `Shutdown` waits, bounded by `engineBootDrainTimeout` (10s),
for an in-flight boot to resolve before concluding there is nothing to stop.

Worth recording *how* it surfaced: the first end-to-end run showed the engine
dying on SIGTERM with no `engine stopped` log line. The obvious reading — that
`Pdeathsig` was racing the graceful path and winning — was wrong. Disabling the
death signal showed the engine leaking outright, which pointed at publication
timing instead. Both the wrong and the right diagnosis produced the same
observable ("engine is gone"); only disabling a layer told them apart.

**The live-test model ids had rotted.** `TestLiveHTTPModelSelection` inherited
`opencode/mimo-v2.5-free` from the ACP test it was ported from, and that id
now fails as `No provider available`. OpenCode's free Zen pool rotates, so the
test resolves its override from the engine's live catalog instead. It picked
`opencode/laguna-s-2.1-free` on the verification run.

### 10.2 Deviations from the plan

- **`internal/cli/service/defaults_mcremote.yaml` was missing from §3.4.** It is
  `//go:embed`-ed and is what every fresh `setup-service` writes, so it was the
  highest-impact of the four config files. §3.4 has been corrected.
- **The `transport` env override needed an explicit `BindEnv`.** §3.3 predicted
  the env path had to be caught; in practice removing the `SetDefault` also
  removed viper's knowledge of the key, so `AutomaticEnv` stopped resolving it
  and the guard silently never fired. `load.go` now binds the retired key
  explicitly, purely so validate can reject it.
- **Commit order.** Commits 3–6 landed while the live suite ran, rather than
  strictly after commit 1. Nothing depended on the ordering.

### 10.3 Live tests are model-dependent

Two of the ported tests assert on behaviour a live model may or may not
exhibit, and both now degrade to `t.Skip` with a reason rather than failing:

- **Permission round-trip** needs the model to actually call the bash tool. It
  sometimes answers without one, requesting no permission and leaving nothing
  to assert. The deterministic plumbing is covered by httpagent's unit tests.
- **Cancel** needs the turn to still be running when the stop arrives. A model
  that finishes first makes cancellation untestable; the test then asserts only
  that a late cancel is a harmless no-op.

This is a deliberate trade: these run under `-tags live_opencode`, are not in
CI, and a failure caused by free-tier latency teaches nothing. A genuine
regression — a permission requested but never resolved, a cancel that errors or
produces an error event — still fails.

### 10.4 Verified end to end

On the dev host, against a real `opencode serve`:

| Layer | Scenario | Result |
|---|---|---|
| 1 — `Pdeathsig` | `kill -9` the daemon | engine died with it |
| 2 — graceful | SIGTERM a daemon with a booted engine | `engine stopped … graceful=true` |
| 2b — boot drain | SIGTERM inside the boot window | no leak (confirmed with the death signal disabled) |
| 3 — startup sweep | planted marked orphan, dead owner | reaped at startup |
| 3 — safety | engine owned by a live daemon | left running by the sweep |
| 3 — safety | unmarked process | never listed, never touched |

`mcremote engines` lists engines with owner and live/ORPHAN state; `--reap`
clears orphans on demand.

### 10.5 Follow-ups not done here

- **Pre-0019 engines carry no marker** and are therefore invisible to the sweep.
  Any engine running at upgrade time must be cleared by hand, once.
- The `providers.opencode.transport` shim field and its `BindEnv` should be
  deleted one release after this ships (§4.1).
- `internal/provider/httpagent` still aliases its `Config` from `acpagent`,
  which now reads oddly given the two transports no longer share a provider.
  Cosmetic; deferred.