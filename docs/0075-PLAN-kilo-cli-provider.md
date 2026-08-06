# Implement MADR 0075 — Kilo CLI as a session provider

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

Associated MADR: [0075-MADR-kilo-cli-provider.md](0075-MADR-kilo-cli-provider.md)

- **Status**: accepted — decisions PD1–PD6 locked by owner 2026-08-06.
  **P1 implemented 2026-08-06**: `IDKilo`, `KiloProviderConfig` + viper wiring,
  `internal/provider/kilo` (dialect, static catalogs, version hook), daemon
  registration; unit tests green, live boot verified (engine prewarm on
  loopback, version 7.4.20 logged, graceful reap, no orphans).
  **P2 implemented 2026-08-06**: full session loop forked from opencode
  (session/lifecycle/permission/question/todo/command/mode/resync/session_ops/
  usage) with the kilo deltas (transient-part filter, `session.turn.close`
  EndTurn, kilo-extra event ignores, PD3 first-slash model split, `code`
  agent); live green: PONG stream, resume, cancel, and the PD6 permission
  round-trip (allow + reject) — raw `permission.asked` fixture committed to
  docs/kilo-spike-7.4.20/sse-permission.raw, MADR Q10 resolved.
  **P3 implemented 2026-08-06**: live catalogs (connected models via
  `/config/providers`, scoped `/provider`, filtered agents, commands),
  AfterBoot default-model + context-limit resolve (live: picked up
  `kilo/kilo-auto/balanced`, the Gateway-authenticated default — PD4 verified),
  MADR 0023 command table, docs (README, config.example.yaml, docs/config.md,
  service defaults template). Live catalog test green (200 models capped,
  181 providers, agents `[ask code debug orchestrator plan]` default `code`).
  **P4 implemented 2026-08-06**: engines help text, service PATH gains
  `~/.cache/kilo/bin`, README Provider: Kilo section, MADR §8 acceptance run
  recorded (all 8 criteria ✅; dual-provider isolation + reap verified live,
  foreign engines untouched). **Deviation:** the P4 doctor step was dropped —
  doctor is service+TCC-only for every provider today, so a kilo-only doctor
  section would be inconsistent one-off surface; a cross-provider doctor
  extension belongs to its own decision. **Plan complete; MADR status
  → implemented, enabled stays false pending the one-week flip criteria.**
- **Date**: 2026-08-06
- **Scope**: Everything required to take `providers.kilo.enabled: true` from config to a
  working phone session — `internal/provider/kilo` dialect, config schema, daemon
  registration, catalogs, command table, live tests (including a live permission
  round-trip, PD6), ops/docs. Maps MADR milestones M1–M4 onto phases P1–P4.
- **Non-goals**: Phone credential writes / OAuth orchestration (MADR 0074 phases);
  **engine password gating** (PD1: kilo serve spawns un-gated on loopback, the same
  trust model as opencode — MADR D5 amended accordingly); `session_tree` default-on
  (blocked on child-SSE fixtures, MADR Q7); Kilo cloud / remote / daemon adoption
  (MADR §2.9); ACP transport (MADR D3); changing OpenCode behavior in any way.
- **Grounding**: Tree at `8e2524d` plus staged MADR 0074/0075 edits (2026-08-06);
  live kilo **7.4.20** spike + re-probes recorded in MADR 0075 (Appendix E). All
  file/line references below verified against this tree.

---

## 0. Grounding — code facts that bound this plan

| Fact | Where (verified) |
| --- | --- |
| `opencode.Config` is a **type alias** of `httpagent.Config` | `internal/provider/opencode/http.go:73` |
| `httpagent.Config` fields: `Bin, AlwaysApprove, DefaultCWD, Model, PermissionTimeout, TurnStallNotice, Pure, SessionTree *bool, StreamCoalesce *time.Duration` | `internal/provider/httpagent/httpagent.go:37–58` |
| `Dialect` contract: `ID, DefaultBin, ServeArgs(port), HealthPath, EventsPath, AfterBoot(ctx, api), DecodeFrame, NewSession` | `httpagent.go:94–115` |
| Optional dialect catalog hooks: `StaticModels/Live…`, `StaticAgents/…`, `StaticCommands/…` | `httpagent.go:178–220` |
| Engine spawn: `exec.Command(p.cfg.Bin, p.dialect.ServeArgs(port)...)`; env appended at one point; stderr ring prefix is `<dialect id>-stderr` automatically | `httpagent/provider.go:406–435` (env `:424`, ring `:434`) |
| **No Authorization header anywhere in httpagent**: health poll `:491`, SSE dial `:625`, REST helper `:874` — under PD1 this stays as-is; the kilo engine spawns un-gated on 127.0.0.1 exactly like opencode | `httpagent/provider.go` |
| Ownership env stamps `MCREMOTE_ENGINE_ID` / `MCREMOTE_ENGINE_OWNER`, process group + death signal | `provider.go:412–426`, `procutil/reap.go:21–24` |
| Provider lifecycle: `EnsureServer` `:294`, `Shutdown` (SIGTERM-first) `:325`, lazy `ensureServer` `:372` | `httpagent/provider.go` |
| Daemon registration template (opencode block): field mapping, explicit `SessionTree`/`StreamCoalesce` pointers, `Ready()` warn, `Prewarm → EnsureServer()` | `internal/daemon/daemon.go:175–210` |
| Well-known IDs end at `IDCodex`; **no `IDKilo`** | `internal/provider/provider.go:36–45` |
| `ModelCatalog` / scoped `ListModelsFor` (MADR 0043) implemented by `httpagent.Provider` | `provider.go:267–299`, `httpagent/provider.go:135–205` |
| Config template: `OpencodeProviderConfig` (incl. `RetiredTransport` guard) | `internal/config/config.go:508–540` |
| Viper wiring template: BindEnv (`MCREMOTE_PROVIDERS_OPENCODE_*`), SetDefault block, `default_cwd` validation | `internal/config/load.go:73, 216, 310–321` |
| OpenCode SSE unwrap to copy | `opencode/http.go:720–739` (`DecodeFrame`) |
| OpenCode live tests build tag | `//go:build live_opencode` (5 files) |
| Fork source inventory (non-test): `http.go` 1617, `lifecycle.go` 447, `permission.go` 388, `mode.go` 311, `resync.go` 283, `session_ops.go` 271, `command.go` 221 | `internal/provider/opencode/` |
| Docs to touch: README providers table (`README.md:598–602`), `configs/config.example.yaml:152–178` (opencode block as template), `docs/config.md` | verified |

Kilo wire facts (argv, paths, Basic Auth behavior, SSE envelope/types, prompt body,
agents, catalog size, auth-state-dependent defaults) are **not** restated here — the
MADR §2.3–§2.8 and Appendices D–E are the source of truth; steps below cite them.

## 0.1 Decisions — locked by owner, 2026-08-06

| ID | Decision | Locked value |
| --- | --- | --- |
| PD1 | Engine auth | **No auth — same as opencode.** `KILO_SERVER_PASSWORD` is never set; the engine runs un-gated on `127.0.0.1` and httpagent stays untouched. Loopback binding is the security boundary (MADR D5 amended; the spike's Basic Auth facts remain documented for operators who gate manually) |
| PD2 | `providers.kilo.session_tree` default | **`false`** (flip criteria = child SSE fixtures, MADR Q7) |
| PD3 | Model id split for `{providerID, modelID}` | **Yes** — split config `model` on **first** `/` only, preserving Kilo `~vendor/model` aliases (`kilo/~anthropic/x` → `kilo` + `~anthropic/x`, MADR §2.4) |
| PD4 | Static model seeds | **Yes** — `kilo/kilo-auto/free` + this-host connected `openrouter/openrouter/free`; never hard-code `kilo-auto/balanced` (auth-state-dependent, MADR §2.6) |
| PD5 | `/provider` catalog handling | **Yes** — live fetch with in-memory cache per engine boot; prefer connected providers from `/config/providers` (4.7 MB payload, MADR §2.4) |
| PD6 | Permission-ask SSE | **Add the live fixture now** — P2 includes a live tool-using turn that forces a real permission ask, captures the SSE frames as fixtures, and round-trips the REST reply (closes MADR Q10 during P2, not later) |

---

## 1. Phase P1 — Provider skeleton (MADR M1)

### Steps

1. `internal/provider/provider.go` — add after `IDCodex` (`:45`):

   ```go
   // IDKilo is the Kilo CLI provider (shared `kilo serve` engine, MADR 0075).
   IDKilo ID = "kilo"
   ```

2. `internal/config/config.go` — add `KiloProviderConfig` to `ProvidersConfig`
   (`Kilo KiloProviderConfig \`mapstructure:"kilo"\``). Fields and defaults per
   MADR §4.2 as amended by PD1: `enabled=false, bin="kilo", always_approve=false,
   default_cwd="", model="", permission_timeout_seconds=120, prewarm=true,
   turn_stall_notice_seconds=120, stream_coalesce_ms=80, session_tree=false,
   pure=false`. No `server_username` (PD1: no engine auth), no `args`, no
   `transport` (mirror the `RetiredTransport` lesson by never introducing the key).
3. `internal/config/load.go` — mirror the opencode block: `BindEnv`
   `MCREMOTE_PROVIDERS_KILO_*` for every key, `SetDefault` group, `default_cwd`
   validation (pattern at `:216`). Extend the config defaults test.
4. `internal/provider/kilo/` — new package, files per MADR §4.1:
   - `kilo.go`: `type Config = httpagent.Config` (same alias pattern as
     `opencode/http.go:73` — PD1 means no extra fields are needed),
     `NewHTTP/NewHTTPWithLogger` constructing
     `httpagent.NewWithLogger(dialect, cfg, log)` (`httpagent/provider.go:91`).
   - `dialect.go`: `ID()=IDKilo`, `DefaultBin()="kilo"`,
     `ServeArgs(port)` → `serve --hostname 127.0.0.1 --port <p> [--pure]`
     (MADR §2.3; never `--mdns` — it rebinds to 0.0.0.0; no
     `KILO_SERVER_PASSWORD`, PD1), `HealthPath()="/global/health"`,
     `EventsPath()="/global/event"`, `DecodeFrame` copied from
     `opencode/http.go:720–739` (same GlobalEvent envelope, MADR §2.4).
   - `catalog.go`: static seeds per PD4; static agents `code` (default), `ask`,
     `debug`, `plan`, `orchestrator`; exclude subagents (`explore`, `general`)
     and hidden (`compaction`, `summary`, `title`) (MADR §2.8).
   - `version.go`: parse `version` from health JSON; log + retain for doctor and
     the future `session_tree` gate. Known-good pin **7.4.20**.
5. `internal/daemon/daemon.go` — copy the opencode block (`:175–210`) with kilo
   fields; same explicit `SessionTree`/`StreamCoalesce` pointer semantics, same
   `Ready()` warn, `Prewarm → EnsureServer()`.
6. Unit tests: `dialect_test.go` (ServeArgs with/without pure; DecodeFrame against
   frames lifted from `docs/kilo-spike-7.4.20/sse-samples.json`); config tests.

### Acceptance

- Daemon boots with `providers.kilo.enabled: true`; `providers.list` shows
  `{id: "kilo", ready: true}` with the binary on PATH, `ready: false` without
  (no crash — registry Ready() is PATH-probe only).
- Spawned engine is bound to `127.0.0.1` on an ephemeral port and carries the
  `MCREMOTE_ENGINE_*` ownership stamps (PD1: un-gated, loopback-only — same
  posture as the opencode engine).

---

## 2. Phase P2 — Session loop (MADR M2)

Fork-and-adapt from `opencode`, file by file (MADR §4.1 prefers copy-then-delete
over a shared flavored dialect):

### Steps

1. `session.go` (from `opencode` `session_ops.go` + parts of `http.go`):
   - Create: `POST /session` with `?directory=` (proven; MADR §2.4).
   - Prompt: `POST /session/{id}/prompt_async` body
     `{parts: [{type: "text", text}], model: {providerID, modelID}, agent}` —
     204 = accepted; model from config `model` split per **PD3**; default agent
     `code` unless config/agent picker overrides.
   - Abort: `POST /session/{id}/abort`; Delete: `DELETE /session/{id}`;
     Resume/replay: `GET /session/{id}` + `GET /session/{id}/message`.
2. Event mapping (adapt `http.go` switch; kilo deltas from MADR §2.4):
   - Core: `message.part.delta` / `message.part.updated` → text; `type: reasoning`
     parts → thought chunks; `session.status` busy/idle + `session.idle` +
     `session.turn.close` → EndTurn; `session.error` → `agenterr.Present` cards.
   - **Filter** parts with `metadata.kilocode.lifecycle: "transient"` (never chat).
   - Ignore for v1: `sync`, `file.watcher.updated`, `session.next.*` switches,
     `message.part.removed` (transient cleanup only).
3. `permission.go` fork: REST reply
   `POST /session/{id}/permissions/{permissionID}` `{response: once|always|reject}`;
   `always_approve` auto-reply and `permission_timeout_seconds` semantics identical
   to opencode.
4. **Live permission fixture (PD6 — do it now, closes MADR Q10):**
   - New `live_permission_test.go` (tag `live_kilo`): with `always_approve` **off**
     and a restrictive `permission` config, drive a tool-using turn (e.g. agent
     `code`, prompt that writes a file under the session cwd) until the engine
     emits a permission ask on `/global/event`.
   - Capture the raw ask frame(s) and commit them (anonymized) under
     `docs/kilo-spike-7.4.20/` as `sse-permission.raw` + a decoder unit fixture.
   - Round-trip the REST reply (`once`) and assert the turn continues to idle;
     repeat with `reject` and assert the turn ends without the tool effect.
   - Update MADR Q10 to **Resolved** with the observed event type/shape.
5. Questions: `POST /question/{id}/reply` with `answers` arrays (MADR §2.4).
6. Live smoke (`live_test.go`, tag `//go:build live_kilo`): health+version; create →
   prompt (`openrouter`/`openrouter/free`, env key present on this host, proven
   PONG turn) → streamed text → idle; abort mid-turn; delete.

### Acceptance

- MADR §8 criteria 2–4 pass on the spike host via `go test -tags live_kilo`,
  including the live permission ask → reply → continue round-trip (PD6).

---

## 3. Phase P3 — Parity (MADR M3)

### Steps

1. Catalogs (`catalog.go` + optional live listers, PD5):
   - `LiveModels`: `GET /config/providers` for connected ids + defaults (kilo's
     default is auth-state-dependent — surface, don't hard-code); full
     `GET /provider` only on scoped `ListModelsFor` (0043) with per-boot cache.
   - `LiveAgents`: `GET /agent` filtered per §2.8. `LiveCommands`: `GET /command`.
2. `commandtable.go`: MADR 0023 matrix — advertise only HTTP-executable commands;
   pure-TUI entries recorded unsupported (MADR §4.7).
3. `mode.go` fork decision: kilo has no OpenCode build/plan mode pair; map the
   agent picker (`code`/`plan`/…) instead — delete mode plumbing that doesn't
   transfer rather than emulating it.
4. `resync.go` fork for reconnect/backfill parity.
5. `session_tree` stays `false` (PD2); do not port `child_suppression` demux until
   Q7 fixtures exist — keep the config key wired so the flip is config-only.
6. Docs: README providers table row (`README.md:598–602` pattern),
   `configs/config.example.yaml` kilo block (template `:152–178`), `docs/config.md`,
   install + known-good version note (`npm i -g @kilocode/cli`, brew tap,
   host binary `/opt/homebrew/bin/kilo`).

### Acceptance

- Phone model/agent/command pickers populate from a live engine; static fallbacks
  render offline; `go test ./...` green.

---

## 4. Phase P4 — Ops + acceptance (MADR M4)

### Steps

1. `engines` CLI / doctor: list owned `kilo serve` processes (ownership env already
   stamped by httpagent); doctor reports binary presence, version vs 7.4.20 pin,
   and `GET /kilo/auth-status` summary (read-only; write paths stay 0074).
2. Service PATH notes: npm global bin, `/opt/homebrew/bin`, `~/.cache/kilo/bin`
   (self-update target, MADR §4.9).
3. Run the full MADR §8 acceptance list end-to-end on the spike host, including
   §8.5 (restart leaves no orphan `kilo serve` — SIGTERM-first Shutdown + death
   signal + reap) and §8.6 (opencode + kilo enabled together, isolated stores).
4. Decide default-enabled flip: **stays `false`**; record flip criteria (M1–M3
   accepted + one week of spike-host use) in the MADR status line.

---

## 5. Verification (each phase, then final)

```text
go build ./...                                        # every phase
go vet ./...                                          # every phase
go test ./...                                         # every phase (unit + fixtures)
go test -tags live_kilo ./internal/provider/kilo/...  # P2+ on a host with kilo 7.4.20
```

Final checklist = MADR §8 items 1–8 verbatim; each must be demonstrably true on the
spike host before the MADR status advances to "implemented".

## 6. Rollout and rollback

- **Rollout**: ships dark — `providers.kilo.enabled` defaults `false`; enabling is
  a per-host config edit. No migration, no protocol change (MADR D6), no phone
  release required (provider appears via existing `providers.list`).
- **Rollback**: set `enabled: false` (engine is reaped on daemon restart via
  Shutdown/death-signal); full code rollback is deleting the `kilo` package,
  `IDKilo`, the config block, and the daemon registration — no other subsystem
  references them.
- **Blast radius guard**: httpagent and the opencode dialect are untouched (PD1),
  so the only shared-code surface is the daemon registration block.
