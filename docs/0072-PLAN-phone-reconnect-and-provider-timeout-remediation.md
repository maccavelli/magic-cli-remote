# MADR 0072 — Implementation plan: reconnect, hang UX, host config, service heal

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: **Implemented** (software P1–P5 + docs P6, 2026-08-05).
  P0 ops applied on primary host (daemon restored, config stall/limits,
  pair prune). Owner decisions locked as in §0.0.
- **Date**: 2026-08-05
- **Source**:
  [0072-MADR-phone-reconnect-and-provider-timeout-incident.md](0072-MADR-phone-reconnect-and-provider-timeout-incident.md)
- **Scope**: Remediate 0072 findings F1–F10 with ops P0, ship already-written
  default/deadline WIP, service-control heal, pair-key twin cleanup, sticky
  session-status resync, residual dial/join hygiene, and docs/config materialization.
- **Non-goals**: Re-open 0065 design (only wire HealStart / doctor signals that
  0065 already named); full 0068 redesign; goose dangerous-auto policy (0069
  closed); APNs / iOS background push; multi-host fleet orchestration.
- **Grounding**: Code + live host forensics as of 2026-08-05; tree HEAD
  `077c979` plus **uncommitted** WIP on config/ws/relay (see §0.2).

---

## 0.0 Accepted decisions (owner 2026-08-05)

| ID | Decision |
| --- | --- |
| **§6.2** | **Option A** — resume fast path still lists + `syncFromMeta`; skip history only |
| **D1** | Codex `turn_stall_notice_seconds` default **120** |
| **D2** | `ws_read_deadline_seconds` default **120** |
| **D3** | WS frame `writeDeadline` **20 s** (not 15 s) |
| **D4** | Re-pair with same client key **revokes** prior device ids |
| **D5** | `HealStart: true` on every `mcremote update` service cycle |
| **D6** | Materialize explicit limits liveness keys in setup templates / new configs |
| **D7** | Serialize DialEpisodes when it adds robustness — **yes, implement in P5** |

## 0. Grounding (code facts that bound the plan)

### 0.1 What the MADR got right (verified in tree)

| Claim | Code / host fact |
| --- | --- |
| LaunchAgent `Stop` = `bootout` (job removed) | `internal/cli/service/control.go` `Stop` → `launchctl bootout gui/$UID/label` |
| `KeepAlive` does not reload a bootout job | launchd model; plist can exist while service domain entry is gone |
| `update.Run` restarts only if **WasActive** | `internal/update/run.go` sets `WasActive: active` only; **`HealStart` is never set** |
| `HealStart` exists and is tested but unused by CLI | `swap.go` + `TestSwapAndRestart_HealEnabledDown`; CLI `update.go` omits it |
| `setup-service` never overwrites existing config | `ensureDefaultConfig` returns early when file exists |
| Host codex stall **0** is a pin, not a default accident | Host YAML explicit; HEAD `Defaults().Codex.TurnStallNoticeSeconds == 0` |
| Stall gated by `> 0` | `acpagent/session.go` `watchStall`, `acphttp/session.go`, `httpagent/session.go` |
| Pair claim always **creates** a new device | `handlePairClaim` → `store.CreateWithClientKey` — no prior-key revoke |
| Prune exists | `mcremote pair prune --stale` / `--keyless` |
| Transcript cache clears stored `running` | `transcript_cache.dart` load path |
| `applyMetaStatus` clears busy when host leaves `running` | `transcript_reducer.dart` |
| Resume fast path can **skip entire resync** including meta | `session_synchronizer.dart` `coveredAndUnchanged` → `return` with **no** `listSessionSnapshot` / `syncFromMeta` |
| DialEpisode single-flight is epoch-based, not join-global | `_connectEpoch` / `_staleAttempt`; park→resume single-join tests exist |
| HEAD write deadline 5 s; target **20 s** (D3) | `internal/ws/server.go` |
| HEAD read deadline default 60 s; WIP 120 s | `config.Defaults` + `ws.New` fallback |
| Doctor is TCC-only | `internal/cli/doctor.go` — no LaunchAgent/loaded/active check |

### 0.2 Uncommitted WIP already in the working tree (ship, do not re-author)

Files (from `git diff` at assessment time):

| Area | Change |
| --- | --- |
| `internal/config/config.go` | Codex stall default **0 → 120**; WS read deadline default **60 → 120** |
| `internal/config/keepalive_test.go`, `ws/*_test.go` | Expect 120 s horizons |
| `internal/ws/server.go` | `writeDeadline` **5 → 20 s** (D3); `New` fallback 120 s |
| `internal/ws/liveness.go` | Comment alignment |
| `internal/relay/server.go` | TLS handshake ErrorLog demoted to Debug (scanner noise) |
| Templates + README | Codex stall 120; `defaults_mcremote.yaml` explicit limits keys |

**Plan rule:** Phase P1 lands this WIP as-is (with any small review fixes), then
`make install` + service restart. Do not rewrite the same constants twice.

### 0.3 MADR analysis — strengthen / correct for planning

| MADR item | Planning correction |
| --- | --- |
| F8 “resync should clear running when host idle” | **Partially already true** via `syncFromMeta` + `applyMetaStatus`. Real gap: **resume fast path skips list**, so status never reconciles when seq bounds match. Plan targets that skip path + a force-status edge. |
| F1 “update left daemon down” | Plausible; not proven sole cause of 17:00. Also covered by any `bootout` without bootstrap. Plan: heal + doctor + ops restore; do not only blame update. |
| F5 tunnel storms | Phone DialEpisode + host concurrent joins both matter; 0068 tests cover park→resume single join but production still showed multi-join. Plan: rate-limit / coalesce residual + hardware validation, not full redesign. |
| Host config stall 0 | Survives forever under setup-service (no merge). Plan needs **ops edit** or a one-shot **config migrate** CLI — not “ship defaults only”. |
| D4 multi-device same key | **Owner decision required** before implementing auto-revoke. Plan has a default recommendation with an opt-out. |

### 0.4 Severity → phase mapping

| Phase | Findings | Class |
| --- | --- | --- |
| **P0** | F1 ops, F2 host pin, F4 prune, F7 host limits keys | Host restore (operator, minutes) |
| **P1** | F2 defaults, F3/F6 deadlines, F10 WIP ship, F7 templates | Land + install tree WIP |
| **P2** | F1 code (HealStart, doctor service, Start after bootout) | Service control harden |
| **P3** | F4 re-pair key twins | Auth store + pair.claim |
| **P4** | F8 sticky running on resume skip | Mobile resync status |
| **P5** | F5 residual join storms | Dial / relay join hygiene |
| **P6** | F9 grok auth warn (optional), docs, ops rows, MADR close | Close-out |

Commit **between phases**. No push unless asked. Go files: `make pre-add-check`
before stage (AGENTS.md).

---

## 1. Phase map (summary)

```text
P0  Ops restore this host (config, launchd, prune, phone)
P1  Ship WIP defaults/deadlines + install binary
P2  Service heal: HealStart on update; doctor service status; Start reliability
P3  Pair: same client_key_fp replaces prior device(s)
P4  Mobile: never skip status reconcile on resume fast path
P5  Reconnect storm residual (join single-flight / rate)
P6  Docs, ops-hardware rows, MADR/plan status, optional grok auth
```

**Critical path for the phone today:** P0 → (optional P1 install) → validate.
Software quality path: P1–P4 required for “won’t recur”; P5–P6 residual.

---

## 2. P0 — Immediate host ops (no product code)

**Owner:** operator on `macos-laptop`.  
**Goal:** phone can connect; codex stall on; one clean device id.

### 2.1 Restore LaunchAgent

```bash
# Preferred (rewrites plist if needed, enable + bootstrap + kickstart)
mcremote setup-service --force

# Verify
launchctl print "gui/$(id -u)/com.magiccliremote.mcremote" | head -40
pgrep -lf mcremote
tail -n 30 ~/Library/Logs/mcremote/mcremote.err.log
# Expect: listening …; registered with mcrelay host_id=macos-laptop
```

If setup-service fails: manual `bootstrap` + `kickstart -k` against
`~/Library/LaunchAgents/com.magiccliremote.mcremote.plist` (see 0058).

### 2.2 Patch host config (required — defaults alone do not help)

Edit `~/.config/mcremote/config.yaml` (setup **never** overwrites it):

```yaml
providers:
  codex:
    turn_stall_notice_seconds: 120   # CHANGE from 0

limits:
  max_ws_clients: 8
  max_live_sessions: 16
  ws_read_deadline_seconds: 120      # ADD (after P1 binary: enforced 120)
  ws_resume_window_seconds: 120      # ADD
```

Leave `allow_full_access: true` unless security posture changes.  
Leave goose/opencode/grok stall/prewarm as-is (already sane).

```bash
launchctl kickstart -k "gui/$(id -u)/com.magiccliremote.mcremote"
```

### 2.3 Pairing prune

```bash
mcremote pair list
# Keep the phone's current device id (post re-pair: prefer 51090d1c… if still
# the live token; confirm via successful auth log line).

# Age out unused rows (tune duration):
mcremote pair prune --stale 48h

# If many never-used duplicates remain, revoke by id:
# mcremote pair revoke <device-id>
```

Do **not** prune the single active id. After prune, force-stop the phone app
once and reconnect so token/device id is consistent.

### 2.4 P0 acceptance

| Check | Pass |
| --- | --- |
| `IsActive` / print state = running | yes |
| Log: `registered with mcrelay` | yes |
| Phone mesh or relay connected ≥ 5 min foreground | yes |
| Codex long silent tool emits stall notice by ~2 min (after config) | yes if turn truly silent |
| `pair list` ≤ few devices for s22+ | yes |

**Commit:** none (ops only). Record result in ops notes or 0072 MADR appendix if desired.

---

## 3. P1 — Ship WIP: defaults, deadlines, templates

**Findings:** F2 (code default), F3, F6, F7 (templates), F10.  
**Goal:** committed tree matches forensics-backed sane defaults; binary installed.

### 3.1 Land working-tree WIP

Review then commit the existing diff as one coherent change (or split if
relay TLS filter should be separate):

| File | Action |
| --- | --- |
| `internal/config/config.go` | Keep codex stall 120; WS read deadline 120; comments |
| `internal/config/keepalive_test.go` | Keep reap-inside-deadline assertion vs 120 |
| `internal/ws/server.go` | writeDeadline **20 s** (D3); New fallback 120 |
| `internal/ws/liveness.go` | Comment only |
| `internal/ws/capacity_retry_test.go`, `negotiation_test.go` | 120 s expectations |
| `internal/relay/server.go` | TLS ErrorLog filter (ops hygiene; ship with P1 or tiny follow-up) |
| `configs/config.example.yaml`, `mesh-grok`, `prod.example` | codex stall 120 |
| `internal/cli/service/defaults_mcremote.yaml` | stall 120 + explicit limits keys |
| `README.md` | codex stall table 120 |

**Do not** add host-specific secrets or `allow_full_access: true` into defaults
templates (stay false in repo templates; host keeps its own pin).

### 3.2 Tests / gates

```bash
go test ./internal/config/ ./internal/ws/ ./internal/relay/ -count=1
make pre-add-check FILES="internal/config/config.go internal/config/keepalive_test.go internal/ws/server.go internal/ws/liveness.go internal/ws/capacity_retry_test.go internal/ws/negotiation_test.go internal/relay/server.go"
# then git add (no -m); git commit  # prepare-commit-msg hook
```

### 3.3 Install on this host

```bash
make install   # or project install path → ~/.local/bin/mcremote
mcremote setup-service --force   # if plist args need refresh
launchctl kickstart -k "gui/$(id -u)/com.magiccliremote.mcremote"
mcremote version   # expect new git describe
```

Confirm log / caps: v2 `read_deadline_ms` = 120000 on next phone auth (if
negotiating v2).

### 3.4 P1 acceptance

- Unit tests green for config/ws/relay touched packages.
- New binary version on host.
- Host still has codex stall **120** in YAML (P0); default matches if key removed.
- No regression: `go test -race ./internal/ws/ ./internal/config/` before commit preferred.

---

## 4. P2 — Service control heal (F1 code)

**Goal:** bootout is never a permanent footgun for product install/update paths;
operators can see “enabled but not loaded”.

### 4.1 Wire `HealStart` on `mcremote update`

**File:** `internal/update/run.go`

```go
SwapAndRestart(..., SwapOpts{
    Product:          opts.Product,
    RestartService:   opts.Service != nil,
    WasActive:        active,
    HealStart:        true, // NEW: enabled-but-stopped → start after swap
    Service:          opts.Service,
    ...
})
```

**Rationale:** `SwapAndRestart` already implements heal; CLI never enabled it.
Matches `install-binary.sh` `want_up` behavior and `TestSwapAndRestart_HealEnabledDown`.

**Tests:** extend `run_test.go` or swap tests: service inactive pre-swap but
plist/managed → Start called when HealStart true via Run path (inject Service).

### 4.2 Harden `service.Start` after bootout

**File:** `internal/cli/service/control.go`

Today: `kickstart -k` then fallback `bootstrap gui/$UID plist`.

Plan:

1. Prefer: if print fails (not loaded) → **bootstrap then kickstart**.
2. If kickstart fails with “no such process” / not found → bootstrap + kickstart.
3. Return wrapped errors with the exact launchctl stderr (already partially true).
4. Unit-test with injected `runLaunchctl` sequence (pattern from `setup_launchd_test.go`).

### 4.3 Doctor: service status section (macOS + Linux)

**File:** `internal/cli/doctor.go` (+ tests)

Add a second section after TCC:

```text
mcremote user service
  plist:   present|missing  <path>
  enabled: yes|no|unknown   (print-disabled / systemctl is-enabled)
  loaded:  yes|no
  active:  yes|no
  hint:    ... setup-service --force | bootstrap ...
```

Use `service.IsActive("mcremote")` + stat of standard plist/unit path + optional
`launchctl print` capture. Keep TCC section unchanged.

### 4.4 Optional: `mcremote service status|start|stop` thin CLI

Only if doctor is insufficient; **prefer doctor first** to avoid new command
surface. If added: thin wrappers over `service.IsActive/Start/Stop`.

### 4.5 P2 acceptance

- Update with service down but plist present ends **active**.
- Doctor reports “enabled, not loaded” on a simulated bootout fixture.
- Existing swap tests still pass; new heal-through-Run test green.
- `make pre-add-check` on touched Go files.

---

## 5. P3 — Pair claim: one active device per client key (F4)

**Owner decision D4 (default recommendation):** when `require_client_key` and
pair.claim enrols a non-empty `client_key_fp`, **revoke other devices with the
same fingerprint** after successful create (or before, transactionally).

### 5.1 Auth store API

**File:** `internal/auth/store.go`

```go
// RevokeByClientKeyFP removes all devices with the given SPKI fingerprint
// except optional keepID. Returns revoked devices.
func (s *Store) RevokeByClientKeyFP(fp string, keepID string) ([]Device, error)
```

- Empty `fp` → no-op (keyless path).
- Persist once; return list for kick notify.

Tests in `store_test.go`: three devices, two share fp → revoke leaves one.

### 5.2 Wire in `handlePairClaim`

**File:** `internal/ws/server.go` after successful `CreateWithClientKey`:

1. If `c.clientKeyFP != ""`, call `RevokeByClientKeyFP(fp, newDev.ID)`.
2. `notify` disconnect for revoked ids if admin path available from ws
   (or log + rely on next auth fail for old tokens). Prefer kick if
   `NotifyDisconnect` is reachable from daemon wiring; otherwise document
   that old tokens fail on next use and prune kicks on CLI.

Also: CLI `pair prune` remains for historical garbage without shared key.

### 5.3 Optional config escape hatch

```yaml
auth:
  single_device_per_client_key: true  # default true when require_client_key
```

Only add if multi-phone same key is a real product need; otherwise hard-code
behavior when require_client_key (simpler).

### 5.4 Tests

- `TestWSClientKeyEnrolAndAuth`: second pair.claim with same cert → device
  count 1 (or 1 current + revoked gone).
- Existing key mismatch tests unchanged.

### 5.5 P3 acceptance

- Re-pair same phone key does not grow `devices.json`.
- Old token cannot auth after revoke.
- `pair list` after lab re-pair stays small.

---

## 6. P4 — Sticky `running` on resume fast path (F8)

**Goal:** host status always wins over local busy after reconnect, without
killing the seq fast path.

### 6.1 Root cause (grounded)

```dart
// session_synchronizer.dart
if (coveredAndUnchanged) {
  return; // skips listSessionSnapshot + syncFromMeta
}
```

Seq equality does **not** imply status equality. A missed `turn_complete` /
status frame with matching latest seq (or status-only drift) leaves composer
busy until app kill.

### 6.2 Fix options — **locked: option A** (owner 2026-08-05)

| Option | Behavior | Decision |
| --- | --- | --- |
| **A** | On resume fast path, still call a **cheap status reconcile**: full `listSessionSnapshot` + `syncFromMeta`; skip only **history** workers when seq covered | **Locked** |
| **B** | Encode status into resume confirmation so mismatch fails fast path | Rejected |
| **C** | Drop fast path entirely (regress 0068 P3 cost) | Rejected |

**Implement A:**

1. When `coveredAndUnchanged`, still:
   - `final snap = await client.listSessionSnapshot();`
   - `transcripts.syncFromMeta(snap.sessions, complete: snap.complete);`
   - **return** without history workers (seq already matched).
2. Or: split snapshot API — if list is already light, (1) is enough.

### 6.3 Strengthen `applyMetaStatus` (if needed)

Already clears `running` → non-running. Add unit test:

- Local transcript `status=running`, meta `idle` → idle + pending cleared.
- Local `running`, meta `running` → unchanged.
- Resume fast path test: seq match + local running + host idle → idle after resync.

### 6.4 Files

| File | Change |
| --- | --- |
| `apps/mobile/lib/state/session_synchronizer.dart` | Always syncFromMeta; skip only history when covered |
| `apps/mobile/test/session_synchronizer_test.dart` | New cases for status-only reconcile |
| `apps/mobile/test/transcript_reducer_test.dart` (or existing) | applyMetaStatus matrix if thin |

### 6.5 P4 acceptance

```bash
cd apps/mobile && flutter test test/session_synchronizer_test.dart
dart format --output=none --set-exit-if-changed lib/state/session_synchronizer.dart
```

Manual: mid-turn kill radio → reconnect → if host idle, composer not stuck;
if host running, stall notice appears (P0/P1).

---

## 7. P5 — Residual reconnect / tunnel storms (F5)

**Goal:** reduce multi-join bursts under failure; do not redesign 0062/0068.

### 7.1 Phone (DialEpisode) — **D7 locked: serialize for robustness**

**Files:** `mcremote_client.dart`, `relay_transport.dart`

Implement (not optional):

1. Confirm park awaits prior closer before new join (0068 P5 / existing
   `relay_lifecycle_test` park→resume).
2. Gate `_runDialEpisode` on an `_episodeInFlight` Completer (or equivalent)
   so episodes are **serialized** — epoch cancel alone is not enough under
   rapid park/resume + dual transport legs (0072 tunnel storms).
3. Tests: rapid `disconnect` + `connect` while previous relay join still open
   → at most one outstanding join token / tunnel; overlapping episode starts
   await or supersede cleanly without two concurrent joins.

### 7.2 Host / relay

| Surface | Action |
| --- | --- |
| `internal/relayhost` | Log device/host correlation on tunnel bridge; optional coalesce |
| `internal/relay` hub | Already has completeTunnel races tested; document ops signature of storms |
| Rate | Optional: per-host join rate limit (only if phone fix insufficient) |

### 7.3 Write deadline

Shipped in P1 (**20 s**, D3). No further change unless hardware still shows
`write_failed` storms.

### 7.4 P5 acceptance

- Unit/widget tests for single outstanding join under rapid resume.
- Ops: reproduce background→foreground 10× without ≥5 tunnels/min in host log.

---

## 8. P6 — Close-out (F9 optional, docs, validation)

### 8.1 Optional grok auth warning (F9)

If sessions succeed without `auth_method_id`, either:

- configure host `providers.grok.auth_method_id`, or
- downgrade log to Debug when create still works (code change in grok provider).

Only if operator noise is material.

### 8.2 Docs

| Doc | Update |
| --- | --- |
| 0072 MADR | Status → Accepted / In progress → Software complete as phases land; link this plan |
| 0072 PLAN | Status per phase |
| `docs/config.md` | Codex stall default 120; limits liveness keys |
| `docs/ops-hardware-validation.md` | Rows for: bootout heal, stall notice, sticky status, re-pair single key |
| `README` service section | bootout vs kickstart; doctor service section |

### 8.3 Full validation matrix (after P0–P4)

| # | Scenario | Pass criteria |
| --- | --- | --- |
| V1 | setup-service --force | running + mcrelay register |
| V2 | bootout then `mcremote update` (or Start heal) | service back without manual bootstrap |
| V3 | doctor | shows service + TCC |
| V4 | phone foreground 15 min mesh | no read_deadline; LinkHealth fresh |
| V5 | background 2 min → resume | reconnect ≤ dial budget; no app kill |
| V6 | relay-only (mesh blocked) | connect; no tunnel storm |
| V7 | codex silent ≥120 s | stall notice once; turn end clears busy |
| V8 | goose long turn + blip | reconnect; status honest |
| V9 | re-pair same phone | devices.json single key twin gone |
| V10 | race suite | `go test -race ./...` pre-commit |

---

## 9. Decision checklist (owner before/during implement)

| ID | Question | Plan default if silent |
| --- | --- | --- |
| **D1** | Codex stall default 120? | **Locked yes** (P1) |
| **D2** | WS read deadline default 120? | **Locked yes** (P1) |
| **D3** | Write deadline? | **Locked 20 s** (P1; was proposed 15 s) |
| **D4** | Re-pair revokes same client_key_fp devices? | **Locked yes** when require_client_key (P3) |
| **D5** | HealStart on every update? | **Locked yes** (P2) |
| **D6** | Materialize limits keys in new configs only? | **Locked yes** (templates); host patched in P0 |
| **D7** | Serialize DialEpisodes globally? | **Locked yes if robustness** — implement in P5 |

---

## 10. File / ownership index

| Phase | Primary paths |
| --- | --- |
| P0 | `~/.config/mcremote/config.yaml`, LaunchAgent, `devices.json`, phone app |
| P1 | `internal/config/*`, `internal/ws/*`, `internal/relay/server.go`, `configs/*`, `defaults_mcremote.yaml`, `README.md` |
| P2 | `internal/update/run.go`, `internal/cli/service/control.go`, `internal/cli/doctor.go`, tests |
| P3 | `internal/auth/store.go`, `internal/ws/server.go` handlePairClaim, tests |
| P4 | `apps/mobile/lib/state/session_synchronizer.dart`, tests |
| P5 | `apps/mobile/lib/data/ws/mcremote_client.dart`, `relay_transport.dart`, optional relayhost |
| P6 | `docs/0072-*`, `config.md`, `ops-hardware-validation.md` |

---

## 11. Out of scope / deferred

| Item | Why |
| --- | --- |
| Migrating **existing** host configs automatically | Dangerous without backup; P0 manual + optional future `mcremote config migrate` |
| Changing goose prewarm default | Host false is intentional cold-start tradeoff |
| Lowering codex permission timeout 900 | Correct for long tools |
| Full 0065 self-update polish | Owned by 0065; P2 only wires HealStart |
| iOS APNs | 0067 non-goal |

---

## 12. Risk register

| Risk | Mitigation |
| --- | --- |
| Host YAML still has stall 0 after P1 | P0 edit; README “existing configs not rewritten” |
| HealStart starts a broken binary loop | kickstart failure returns error; doctor shows crash |
| D4 revokes tablet sharing phone key | Product assumes one keystore per physical device; document |
| Resume always lists (P4) adds RTT | One list call is the 0068 baseline cost when not resumed; fast path still skips history |
| 20 s write deadline masks slow peer | Prefer over false freeze; TCP keepalive still reaps |

---

## 13. Suggested commit train

1. `fix(config,ws,relay): 120s read deadline, 20s write, codex stall default` (P1)
2. `fix(update,service,doctor): heal LaunchAgent after bootout; service doctor` (P2)
3. `fix(auth,ws): revoke prior devices on same client key at pair.claim` (P3)
4. `fix(mobile): resync status on resume fast path` (P4)
5. `fix(mobile): serialize dial episodes / join single-flight` (P5; D7)
6. `docs: 0072 plan status + config/ops validation` (P6)

Each commit: pre-add clean for Go; flutter format/analyze for Dart; no
`git commit -m` (hook generates message); no push unless asked.

---

## 14. Implementation order for the implementer

```text
Day 0 (ops):     P0 restore host + phone
Day 0–1 (code):  P1 land WIP + install
Day 1:           P2 HealStart + doctor
Day 1–2:         P3 pair key revoke
Day 2:           P4 session_synchronizer status
Day 2–3:         P5 DialEpisode serialize (D7) + storm residual
Day 3:           P6 docs + V1–V10 matrix
```

---

## 15. Definition of done (0072 closed)

- [x] P0 complete on primary host (running, config, prune)
- [x] P1 merged and installed; defaults match MADR matrix
- [x] P2: update heals down service; doctor reports service state
- [x] P3: re-pair does not accumulate key twins
- [x] P4: resume + host idle clears sticky running without app kill
- [x] P5: DialEpisode serialization (D7); relay lifecycle + dial tests green
- [x] P6: MADR/plan status updated
- [x] Touched package tests green (auth/ws/cli/update/config; session_synchronizer; dial/relay)

Hardware V4–V10 residual remains operator validation on the phone (not
blocking software close).

---

## 16. Implementation record (2026-08-05)

| Phase | Result |
| --- | --- |
| P0 | `setup-service --force`; codex stall 120 + limits keys; prune → 1 device; mcrelay registered |
| P1 | Defaults/deadlines + templates; write **20 s**; install `0.8.0.4.g13de0ff` |
| P2 | `HealStart` on update; Start bootstrap-after-bootout; doctor service section |
| P3 | `RevokeByClientKeyFP` at pair.claim + kick |
| P4 | Resume path lists + `syncFromMeta`; skip history only |
| P5 | `_episodeInFlight` serializes DialEpisodes |
| P6 | This close-out |

---

## 17. Document history

| Date | Note |
| --- | --- |
| 2026-08-05 | Initial comprehensive plan for review; grounded against HEAD+WIP and live host forensics |
| 2026-08-05 | Owner locked decisions; implemented P0–P6 |
