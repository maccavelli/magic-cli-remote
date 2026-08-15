---
status: draft
date: 2026-08-15
associated-madr: "0089-MADR-long-running-session-stability.md"
owner: [Implementer]
target-milestone: [P0 prewarm defaults + phone toggle first; then D1–D4]
---

# Plan: Long-running session stability (Doze, failover, prewarm, cgroups)

## Executive Summary & Goal

* **Associated Decision Record**: [0089-MADR-long-running-session-stability.md](./0089-MADR-long-running-session-stability.md)
* **Goal**: Implement D1–D7 so (a) a locked Android phone does not fake
  a mesh death, (b) a host restart/OOM does not erase the session and
  the user's job, and (c) **no agent engine is started until a session
  needs it or the operator turns Pre-warm on from the phone**.
* **Success Criteria** (must all pass; mapped to MADR Confirmation):
  * [ ] C8: `config.Defaults()` `Prewarm==false` for grok, goose, opencode, codex, kilo
  * [ ] C9: Settings → Providers → `<agent>` switch writes `providers.<id>.prewarm` and applies live
  * [ ] C10: `set_prewarm false` never kills a live session
  * [ ] C1–C2: stall ≠ transport death; relay hop is not sticky
  * [ ] C3–C4: boot rehydrate + job scope survives `systemctl --user restart mcremote`
  * [ ] C5–C7: MemoryHigh written; honesty toast; cross-provider tests
  * [ ] `go test -race ./internal/config/ ./internal/ws/ ./internal/session/ ./internal/daemon/ ./internal/cli/service/` clean
  * [ ] `flutter test` for the listed Dart files clean
  * [ ] `make pre-add-check FILES="<touched .go>"` clean before any `git add`

**Execution order is mandatory.** Do not start Phase 2 until Phase 1
gates pass. Do not start Phase 3 until Phase 2 gates pass. One
in-progress phase at a time.

**Existing key, not a new one.** The YAML / JSON / Go field is
`prewarm` (`mapstructure:"prewarm"`). UI copy is "Pre-warm engine".
Do **not** add `pre-warm`, `pre_warm`, or a second key.

---

## Prerequisites & Dependencies

* **Infrastructure**: host with `systemctl --user`, paired Android
  phone, kilo + at least one of grok/opencode on PATH. Swap on 8 GiB
  hosts is **ops**, not a code gate.
* **Dependencies**: `gopkg.in/yaml.v3` is already in `go.mod` (used by
  config). `flutter_foreground_task` already exposes `allowWakeLock` /
  `allowWifiLock`. No new packages.
* **Pre-Flight Checks** (run once, record output in the implementation
  notes; do not proceed if any fail):
  * [ ] `rg -n 'Prewarm:\\s+true' internal/config/config.go` shows grok, opencode, kilo
  * [ ] `rg -n 'prewarm: true' internal/cli/service/defaults_mcremote.yaml configs/`
  * [ ] `rg -n 'allowWakeLock: false' apps/mobile/lib/data/notifications/foreground_service.dart`
  * [ ] `rg -n 'WAKE_LOCK' apps/mobile/android/app/src/main/AndroidManifest.xml` is comment-only
  * [ ] `ProviderInfoPayload` in `internal/protocol/messages.go` has no `Prewarm` field
  * [ ] `ProviderDetailScreen` has no Switch for prewarm
  * [ ] `config.Write` / surgical YAML helper does not exist (confirm with `rg 'func Write' internal/config`)

---

## Architecture & Technical Design Summary

```
[ ProviderDetailScreen ]
   Switch "Pre-warm engine"
        │ providers.set_prewarm {provider_id, prewarm}
        ▼
[ ws.Server.handleProvidersSetPrewarm ]
   1. validate provider_id ∈ {grok,goose,opencode,codex,kilo}
   2. config.SetProviderPrewarm(id, value)     // surgical YAML
   3. registry.ApplyPrewarm(id, value)
        true  → inst.EnsureServer()
        false → if liveSessions(id)==0 { inst.Shutdown() }
                else latch stopWhenIdle[id]=true
   4. broadcast providers.prewarm {provider_id, prewarm, engine, stopping?}
        │
        ▼
[ serve boot ]
   for each enabled provider:
     if cfg.Providers.<P>.Prewarm { EnsureServer() }   // all default false
```

**Surgical YAML contract** (D7): load the config file with
`yaml.v3.Node` (keeps comments), walk `providers` → `<id>` mapping,
set/create a `prewarm` scalar bool, write to `<path>.tmp` mode 0600,
`rename` over the path. Refuse if the document is not a mapping or
`providers.<id>` is a non-mapping. Never call `viper.WriteConfig`.

**Idle-stop latch**: a `map[provider.ID]bool` on the session manager
or registry. `Manager.Close` / last-session-of-provider path checks
the latch and calls `Shutdown` if set. Latch clears after Shutdown
or after a subsequent `set_prewarm true`.

---

## Phased Implementation Plan

### Phase 0 — Inventory freeze (no behaviour change)

* **Objective**: pin the files and current values so later diffs are
  reviewable and no extra file is touched.
* **Done when**: this table is accurate against HEAD.

| ID | Path | Role in 0089 |
| --- | --- | --- |
| G1 | `internal/config/config.go` | `Defaults().Providers.*.Prewarm` |
| G2 | `internal/config/load.go` | `v.SetDefault("providers.*.prewarm", …)` |
| G3 | `internal/config/config_test.go` | `TestDefaultsGrokPrewarm` + kilo/opencode asserts |
| G4 | `internal/cli/service/defaults_mcremote.yaml` | setup-service seed |
| G5 | `configs/config.example.yaml` | docs |
| G6 | `configs/config.mesh-grok.yaml` | docs |
| G7 | `configs/config.prod.example.yaml` | docs |
| G8 | `docs/config.md` | table rows |
| G9 | `README.md` | prewarm rows (~748, ~818) |
| G10 | `internal/daemon/daemon.go` | `if cfg.Providers.*.Prewarm { EnsureServer() }` |
| G11 | `internal/protocol/messages.go` | `ProviderInfoPayload`, new types |
| G12 | `docs/protocol-v1.md` | client→server table |
| G13 | `internal/ws/server.go` | `handleProvidersList`, new handler |
| G14 | `apps/mobile/lib/data/protocol/models.dart` | `ProviderInfo` |
| G15 | `apps/mobile/lib/data/ws/mcremote_client.dart` | `listProviders`, new RPC |
| G16 | `apps/mobile/lib/features/settings/provider_detail_screen.dart` | switch |
| G17 | `apps/mobile/test/provider_detail_screen_test.dart` | widget test |
| G18 | `apps/mobile/android/app/src/main/AndroidManifest.xml` | WAKE_LOCK (Phase 3) |
| G19 | `apps/mobile/lib/data/notifications/foreground_service.dart` | wake lock (Phase 3) |
| G20 | `internal/cli/service/mcremote.user.service.tmpl` | Memory* (Phase 4) |

Do not edit any path not in this table plus the test files named in
each task, plus `internal/config/` new `prewarm_write.go` /
`prewarm_write_test.go`, plus `internal/provider/registry.go` if the
latch lives there.

---

### Phase 1 — D5: default `prewarm: false` (config only)

* **Objective**: a freshly generated config and an omitted key both
  mean "do not boot engines at `serve` start."
* **Does not**: rewrite `~/.config/mcremote/config.yaml` on this host.
  That file already has explicit `prewarm: true` for grok/opencode/kilo
  and will keep them until Phase 2's toggle (or a manual edit).
* **Tasks** (do in this order; each has a verify command):

  - [ ] **T1.1** Change `Defaults()` in `internal/config/config.go`:
        `Grok.Prewarm`, `Opencode.Prewarm`, `Kilo.Prewarm` from `true`
        to `false`. Leave goose/codex `false`. Update the three
        comments that say "Prewarm default on".
        Verify: `rg -n 'Prewarm:\\s+true' internal/config/config.go`
        prints **nothing**.

  - [ ] **T1.2** Invert tests in `internal/config/config_test.go`:
        * `TestDefaultsGrokPrewarm` — `if d.Providers.Grok.Prewarm { t.Fatal(...) }`
        * kilo block around the current `t.Fatal("kilo prewarm should default true")`
        * opencode block `t.Fatal("prewarm should default to true")`
        Add `TestDefaultsAllPrewarmFalse` that ranges
        `{Grok,Goose,Opencode,Codex,Kilo}` and fails on any `true`.
        Verify: `go test ./internal/config/ -count=1`.

  - [ ] **T1.3** Flip seed/examples:
        `internal/cli/service/defaults_mcremote.yaml`,
        `configs/config.example.yaml`,
        `configs/config.mesh-grok.yaml`,
        `configs/config.prod.example.yaml`
        — every `prewarm: true` → `prewarm: false`. Keep comments;
        rewrite them to "off: engine starts on first session. Turn
        on from the phone (Settings → Providers) or set true here."
        Verify: `rg -n 'prewarm: true' internal/cli/service/defaults_mcremote.yaml configs/`
        prints **nothing**.

  - [ ] **T1.4** Docs only (same commit as T1.1–T1.3): `docs/config.md`
        and `README.md` rows that say default `true` for grok/opencode/kilo
        become default `false`. One-line errata under 0019/0075 is
        **not** this phase (T5.4).
        Verify: `rg -n 'prewarm.*[Dd]efault `true`' docs/config.md README.md`
        prints nothing for those three agents.

  - [ ] **T1.5** Daemon spy test: in `internal/daemon/daemon_test.go`
        (or a new `prewarm_boot_test.go`), construct `config.Defaults()`,
        run the provider-registration fragment (or a extracted
        `maybePrewarm(cfg, registry)` helper if the current `Run`
        function is too large to call). Assert `EnsureServer` is
        **not** invoked for any provider. If extracting a helper,
        the helper signature is:

        ```go
        func startEnabledProviders(ctx context.Context, cfg config.Config, log *slog.Logger) (registry, shutdown func(), err error)
        ```

        and `Run` calls it. Do not change `if cfg.Providers.X.Prewarm`
        semantics beyond honouring the new default.
        Verify: `go test ./internal/daemon/ -count=1`.

* **Phase 1 gate**: T1.1–T1.5 green. `make pre-add-check FILES=` the
  touched Go files. **Stop.** Do not implement the RPC in this phase.

---

### Phase 2 — D7: wire + persist + phone switch

* **Objective**: a connected phone can flip prewarm per agent; the
  host writes one YAML key and starts or latches-stops the engine.
* **Wire contract** (lock these strings; tests compare them exactly):

  ```
  C→S  providers.set_prewarm
       { "provider_id": "kilo", "prewarm": true }

  S→C  ok
       { "provider_id": "kilo", "prewarm": true,
         "engine": "running" | "stopped" | "stopping_when_idle" }

  S→C  error
       code = unsupported | unknown_provider | config_write_failed
              | provider_not_ready

  S→C  providers.prewarm          (push, no req id)
       { "provider_id": "kilo", "prewarm": true,
         "engine": "running" | "stopped" | "stopping_when_idle" }

  providers.list_result entry (additive)
       { "id": "kilo", "ready": true, "prewarm": false, "auth": … }
  ```

  `prewarm` on `list_result` is a JSON bool, **always present** on
  daemons that implement D7 (so the phone can distinguish "false"
  from "old daemon"). Old daemons omit the field; the phone treats
  omit as "unknown" and disables the switch.

* **Tasks**:

  - [ ] **T2.1 Protocol types** in `internal/protocol/messages.go`:
        * add `TypeProvidersSetPrewarm = "providers.set_prewarm"`
        * add `TypeProvidersPrewarm = "providers.prewarm"`
        * add `Prewarm bool \`json:"prewarm"\`` to `ProviderInfoPayload`
          (not `omitempty` — see contract)
        * add

          ```go
          type ProvidersSetPrewarmPayload struct {
              ProviderID string `json:"provider_id"`
              Prewarm    bool   `json:"prewarm"`
          }
          type ProvidersPrewarmPayload struct {
              ProviderID string `json:"provider_id"`
              Prewarm    bool   `json:"prewarm"`
              Engine     string `json:"engine"` // running|stopped|stopping_when_idle
          }
          ```
        * add the request to `docs/protocol-v1.md` client→server table
          on the line after `providers.list`.
        Verify: `go test ./internal/protocol/ -count=1`.

  - [ ] **T2.2 Surgical YAML writer** `internal/config/prewarm_write.go`:

        ```go
        // SetProviderPrewarmFile updates providers.<id>.prewarm in path.
        // Creates the key if missing. Preserves comments and unknown keys.
        // Atomic: write path+".tmp" (0600) then rename.
        func SetProviderPrewarmFile(path, providerID string, prewarm bool) error
        ```

        Allowed `providerID`: `grok`, `goose`, `opencode`, `codex`,
        `kilo` only. Reject anything else with a typed error
        `ErrUnknownProvider`. Tests in `prewarm_write_test.go`:
        1. file with comments + `prewarm: true` → becomes `false`;
           comment on a sibling key still in the bytes
        2. file with no `prewarm` key under `providers.kilo` → key
           inserted, other providers untouched
        3. missing file → error (do not create a new config)
        4. `providerID="nope"` → `ErrUnknownProvider`
        5. concurrent two writes: last rename wins; file always valid
           YAML (use a mutex in the writer)
        Verify: `go test ./internal/config/ -count=1`.

  - [ ] **T2.3 Apply + latch** on the registry (or a tiny new type
        `internal/provider/prewarm.go` used by `daemon` + `ws`):

        ```go
        type PrewarmController interface {
            Set(ctx context.Context, id provider.ID, on bool) (engine string, err error)
            OnSessionClosed(id provider.ID) // honour stopWhenIdle
        }
        ```

        Semantics (deterministic):
        | incoming | live sessions for id | action | `engine` |
        | --- | --- | --- | --- |
        | true | any | `EnsureServer()`; clear latch | `running` |
        | false | 0 | `Shutdown()`; clear latch | `stopped` |
        | false | ≥1 | set latch; do not Shutdown | `stopping_when_idle` |

        Hook `OnSessionClosed` from `Manager.closeMatching` after the
        session is removed, counting remaining live sessions of that
        provider. If zero and latch set → `Shutdown`.
        Tests: table-driven with a fake engine that records
        Ensure/Shutdown calls. Cover the three rows plus "close last
        session with latch" and "set true while latched".
        Verify: `go test ./internal/provider/ ./internal/session/ -count=1`.

  - [ ] **T2.4 WS handler** `handleProvidersSetPrewarm` in
        `internal/ws/server.go`, dispatched next to
        `handleProvidersList`. Sequence:
        1. decode payload; empty `provider_id` → `unknown_provider`
        2. `SetProviderPrewarmFile(cfgPath, id, value)` — cfgPath is
           the path `serve` already opened (thread through Server
           options; do not re-resolve XDG)
        3. `ctrl.Set(ctx, id, value)`
        4. write `ok` with `ProvidersPrewarmPayload`
        5. `Broadcast` `providers.prewarm` to every client
        6. `handleProvidersList` copies `cfg.Providers.<id>.Prewarm`
           **after a live re-read from the controller**, not from the
           boot-time `cfg` snapshot (otherwise the next list is stale)
        Tests in `internal/ws/prewarm_test.go` (new):
        * set true → list shows `prewarm:true` and fake Ensure called
        * set false with a live session → `engine=stopping_when_idle`,
          Shutdown **not** called
        * unknown id → `unknown_provider`
        * missing config path / write fail → `config_write_failed`
        Verify: `go test ./internal/ws/ -count=1`.

  - [ ] **T2.5 Dart models + client**:
        * `ProviderInfo` gains `final bool? prewarm;` — `null` means
          old daemon. Parse: if key absent → null; if bool → value.
        * `McremoteClient.setProviderPrewarm(String id, bool value)`
          sends `providers.set_prewarm`, returns the `engine` string.
        * `McremoteClient.providerPrewarm` stream listens for
          `providers.prewarm` pushes (mirror `providerAuthStatus`).
        Tests: `apps/mobile/test/mcremote_client_test.dart` (or a new
        `provider_prewarm_test.dart`) with the existing fake socket —
        list without key → `prewarm==null`; list with `false` →
        `false`; set round-trip.
        Verify: `flutter test test/provider_prewarm_test.dart test/mcremote_client_test.dart`.

  - [ ] **T2.6 UI** on `ProviderDetailScreen`, **Session defaults**
        section, immediately under Default mode:

        ```
        SwitchListTile
          key: Key('provider-prewarm-${p.id}')
          title: 'Pre-warm engine'
          subtitle: prewarm==null
            ? 'Host does not support pre-warm control'
            : 'Start this agent when mcremote starts. Uses RAM even with no session.'
          value: p.prewarm ?? false
          onChanged: prewarm==null || !_connected ? null : _setPrewarm
        ```

        `_setPrewarm`:
        1. optimistic setState
        2. `client.setProviderPrewarm(id, v)`
        3. on error: revert, `showTopNotification` with `friendlyOpError`
        4. on `stopping_when_idle`: keep switch **off** (requested
           state) and set subtitle to "Stops when the last session
           ends"
        Subscribe to `providerPrewarm` in `initState` (same pattern as
        `_authStatusSub`) and refresh the switch.
        Widget tests in `provider_detail_screen_test.dart`:
        * ready provider + `prewarm:false` → switch off, enabled
        * tap → client method called once with `true`
        * `prewarm:null` → switch disabled, support subtitle
        * push `providers.prewarm` → switch updates without a reload
        Verify: `flutter test test/provider_detail_screen_test.dart`.

  - [ ] **T2.7 In-memory cfg**: after a successful write, update the
        running `config.Config` so a later `daemon` path that reads
        `cfg.Providers.Kilo.Prewarm` sees the new value. Do this in
        the same mutex as the YAML writer. Test: Set false, then
        `cfg.Providers.Kilo.Prewarm==false` without reload.
        Verify: included in T2.4 tests.

* **Phase 2 gate**: T2.1–T2.7 green, `make pre-add-check` on touched
  Go files, `flutter test` of the two new/updated Dart files.
  Manual (not blocking merge): on this host, toggle kilo off, confirm
  `rg prewarm ~/.config/mcremote/config.yaml` shows `false` under
  `kilo:` and the kilo process exits after the last session closes.

---

### Phase 3 — D1 + D2: Android keep-alive and stall classification

* **Objective**: screen lock does not produce `read_deadline` and does
  not sticky-failover to relay.
* **Tasks**:

  - [ ] **T3.1** Re-add
        `<uses-permission android:name="android.permission.WAKE_LOCK"/>`
        to `AndroidManifest.xml`. Replace the 0084 D3 "removed" comment
        with: "Held only while connected or a session is running (MADR
        0089 D1). Not held when parked."
        Verify: `rg WAKE_LOCK apps/mobile/android/app/src/main/AndroidManifest.xml`
        shows a real `uses-permission` line.

  - [ ] **T3.2** `ForegroundServiceController` gains
        `setLocks({required bool wake, required bool wifi})` that
        re-inits / updates `ForegroundTaskOptions(allowWakeLock,
        allowWifiLock)`. Callers:
        * `NotificationCoordinator._onConn`: wake+wifi **true** iff
          `state==connected || state==reconnecting`
        * a new listener on session status: if any transcript
          `status==running`, force wake true even if we later add a
          parked-connected mode
        * parked / disconnected / userLoggedOut → both false
        Default remains false until the first connected edge.
        Tests: unit-test the boolean policy in a new
        `keep_alive_policy.dart` + `keep_alive_policy_test.dart`
        (pure function, no plugin). Table:

        | state | anyRunning | wake | wifi |
        | --- | --- | --- | --- |
        | connected | false | true | true |
        | connected | true | true | true |
        | reconnecting | * | true | true |
        | disconnected | * | false | false |
        | error | * | false | false |

        Verify: `flutter test test/keep_alive_policy_test.dart`.

  - [ ] **T3.3** Stall vs death in `mcremote_client.dart`:
        * add `enum TransportLoss { stall, death }`
        * `_noteTransportDeath({required TransportLoss loss})`
        * `read_deadline` **or** (`_missedPings>=2` and last lifecycle
          was pause/hide) → `stall`
        * everything else (RST, probe fail, `peer_closed` after vpn
          drop) → `death`
        * `_resolvePrimaryTransport`: on `stall`, return the **same**
          transport; do not set `_diedOnTransport` to the other
        * `_recordTransportSuccess`: if the episode was a stall hop
          that landed on the alternate, **do not** persist sticky
        Tests in `dial_episode_test.dart` / `lifecycle_policy_test.dart`:
        1. stall + mesh configured → next primary is mesh
        2. death + both configured → next primary is the other
        3. stall hop that used relay does not call
           `setLastTransportSuccess(relay)`
        Verify: `flutter test test/dial_episode_test.dart test/lifecycle_policy_test.dart`.

* **Phase 3 gate**: T3.1–T3.3 tests green. Hardware soak (C1) is
  Phase 5, not this gate.

---

### Phase 4 — D3 + D4 + D6: restore, isolate jobs, honesty

* **Objective**: host restart does not look like "session gone", and
  a user's compile survives the unit's OOM/kill domain.
* **Tasks**:

  - [ ] **T4.1 Durable last-live-status.** If `CloseAll` must keep
        writing `status=disconnected`, add `last_live_status` +
        `closed_at` on the store record. Restore window: `closed_at`
        younger than `limits.session_restore_seconds` (new key,
        default **300**, floor 30, 0 disables D3).
        Test: `manager_durable_test.go` — create, CloseAll, new
        Manager, `List()` shows `Live==true` for that id.
        Verify: `go test ./internal/session/ -count=1`.

  - [ ] **T4.2 Boot rehydrate.** After provider registration, before
        accepting WS, call `mgr.RestoreRecent(ctx, window)`. For each
        row: `Create` with persisted `AgentSessionID` / `LocalSessionID`.
        Engine start is a side effect of `Create` (D5: only that
        provider). On success emit `TypeNotice` text exactly
        `Host restarted — in-flight turn was interrupted.`
        (`StopReason` `turn_interrupted` if the last status was
        `running`). Failure of one row logs and continues.
        Test: restore with missing binary → that row stays
        disconnected, others restore.
        Verify: `go test ./internal/session/ ./internal/daemon/ -count=1`.

  - [ ] **T4.3 Job / engine isolation** (Linux first):
        1. Unit template: add
           `Delegate=yes`, `MemoryAccounting=yes`,
           `MemoryHigh=3G` (comment: raise on ≥16 GiB hosts),
           no `MemoryMax` by default (document the key).
        2. `setup-service` **additive merge**: if the installed unit
           lacks these four lines, insert them without `--force`
           rewriting the whole file. If live sessions exist (admin
           socket `sessions.list` returns any `live`), print
           `restart required — wait for running sessions` and **do
           not** restart.
        3. Launch engines (`kilo serve`, `opencode serve`, `goose serve`,
           `codex` app-server, grok spare) via
           `systemd-run --user --scope --unit=mcremote-engine-<id>.scope`
           when `XDG_RUNTIME_DIR` is set and `systemd-run` exists;
           else today's `Setpgid` path and log `job_scope=unavailable`.
        4. macOS: no slice; keep process group; log the same
           `job_scope=unavailable`. Tests skip the systemd-run branch
           unless `MCREMOTE_TEST_SYSTEMD=1`.
        Verify: `go test ./internal/cli/service/ ./internal/procutil/ -count=1`.

  - [ ] **T4.4 Honesty (D6).**
        * Chat: on `session_not_live` during an open chat, call
          `resumeSession` **once**. Success → notice
          `Host restarted — session reattached; in-flight turn was interrupted.`
          Failure → existing friendly string.
        * httpagent: map kilo/opencode `background_process.updated`
          to `event.TypeWorkItems` (reuse the work-items panel).
        * ACP: a tool lasting > `turn_stall_notice_seconds` already
          emits a stall; also upsert a work-item with id = tool call
          id.
        Tests: `mc_exception_test.dart` (auto-resume path), a new
        httpagent work-item test with a fixture
        `background_process.updated` frame.
        Verify: `flutter test test/mc_exception_test.dart` and
        `go test ./internal/provider/kilo/ ./internal/provider/httpagent/ -count=1`.

* **Phase 4 gate**: T4.1–T4.4 unit/race green. Chaos restart (C3/C4)
  is Phase 5.

---

### Phase 5 — Verification, soak, cutover

* **Objective**: prove C1–C10 on this class of host; write errata;
  do not bounce a live turn to install the unit.

  - [ ] **T5.1** Additive unit merge on this host. Confirm
        `systemctl --user cat mcremote` contains `Delegate=yes` and
        `MemoryHigh=3G`. Do **not** restart if kilo session is
        `running`.
  - [ ] **T5.2** Screen-lock soak: 10 min, live turn, mesh stays
        `activeTransport`. Host journal has no `reason=read_deadline`
        for the phone's `device_id`.
  - [ ] **T5.3** `systemctl --user restart mcremote` mid-job: job
        scope still in `systemctl --user list-units 'mcremote-engine-*'`;
        session rehydrates; notice present.
  - [ ] **T5.4** One-line errata on 0084 D3, 0063 D6, 0019 §4.2,
        0012 §4.2, 0075 pointing at 0089 D1/D2/D5. No secrets in
        those lines.
  - [ ] **T5.5** Optional ops (not code): add a swap file on 8 GiB /
        0-swap hosts; Samsung: Settings → Battery and device care →
        Battery → Background usage limits → add Magic CLI Remote to
        **Never sleeping apps** and set the app's Battery to
        **Unrestricted**. The global "Put unused apps to sleep"
        toggle is not required.

* **Phase 5 gate**: C1–C10 checked off in the MADR Confirmation
  section (edit the MADR status only after the owner accepts).

---

## Verification & Testing Strategy

| Test Level | Exact command | Pass |
| :--- | :--- | :--- |
| Config defaults | `go test ./internal/config/ -count=1` | T1.2 + T2.2 |
| Daemon boot | `go test ./internal/daemon/ -count=1` | T1.5 |
| Wire | `go test ./internal/ws/ -count=1` | T2.4 |
| Session restore | `go test -race ./internal/session/ -count=1` | T4.1 T4.2 |
| Service template | `go test ./internal/cli/service/ -count=1` | T4.3 |
| Phone models / client | `flutter test test/provider_prewarm_test.dart test/mcremote_client_test.dart` | T2.5 |
| Phone UI | `flutter test test/provider_detail_screen_test.dart` | T2.6 |
| Keep-alive / stall | `flutter test test/keep_alive_policy_test.dart test/dial_episode_test.dart test/lifecycle_policy_test.dart` | T3.2 T3.3 |
| Pre-add | `make pre-add-check FILES="…touched.go…"` | gofmt + golint + govulncheck |
| Soak | T5.2 T5.3 on hardware | C1 C3 C4 |

No `go test -tags live_*` in the merge gate. Live kilo/grok is
acceptance only (T5.2/T5.3).

---

## Rollback & Mitigation Procedures

* **Trigger**: first-session latency unacceptable; YAML writer corrupts
  config; switch kills a live engine; wake lock drains battery idle;
  `Delegate=` leaks scopes.
* **Steps** (in order, stop at the first that restores service):
  1. Phone: turn **Pre-warm engine** off for every agent. Confirm
     engines exit after last session (`ps -o rss,cmd -C kilo,opencode`).
  2. Host: set `providers.*.prewarm: false` by hand if the writer
     failed; `systemctl --user restart mcremote` only when no session
     is `running`.
  3. Kill-switch D3: `limits.session_restore_seconds: 0`.
  4. Kill-switch D1: Settings row (T3.2) off → parked lock policy.
  5. `setup-service --force` to the pre-0089 unit template if
     `Delegate=` misbehaves. Restart only when idle.
  6. APK rollback: previous build; D7 fields are additive — old phone
     on new daemon still works (switch hidden).

---

## Task Progress Checklist

- [ ] **Phase 0**: inventory table matches HEAD
- [ ] **Phase 1 — D5 defaults**
  - [ ] T1.1 `Defaults()` all `Prewarm=false`
  - [ ] T1.2 invert + add `TestDefaultsAllPrewarmFalse`
  - [ ] T1.3 seed + example YAML
  - [ ] T1.4 config.md + README
  - [ ] T1.5 daemon EnsureServer spy
  - [ ] Phase 1 gate
- [ ] **Phase 2 — D7 phone toggle**
  - [ ] T2.1 protocol types + protocol-v1.md
  - [ ] T2.2 surgical YAML writer + tests
  - [ ] T2.3 PrewarmController + idle latch
  - [ ] T2.4 WS handler + list freshness
  - [ ] T2.5 Dart models / client / stream
  - [ ] T2.6 ProviderDetailScreen switch
  - [ ] T2.7 in-memory cfg update
  - [ ] Phase 2 gate
- [ ] **Phase 3 — D1 D2 Android**
  - [ ] T3.1 WAKE_LOCK permission
  - [ ] T3.2 lock policy + FGS wiring
  - [ ] T3.3 stall vs death + sticky
  - [ ] Phase 3 gate
- [ ] **Phase 4 — D3 D4 D6 host**
  - [ ] T4.1 last_live_status
  - [ ] T4.2 boot rehydrate
  - [ ] T4.3 systemd Delegate + scopes
  - [ ] T4.4 auto-resume + work-items
  - [ ] Phase 4 gate
- [ ] **Phase 5 — soak / errata**
  - [ ] T5.1 additive unit merge
  - [ ] T5.2 screen-lock soak
  - [ ] T5.3 restart-mid-job
  - [ ] T5.4 errata pointers
  - [ ] T5.5 ops notes
