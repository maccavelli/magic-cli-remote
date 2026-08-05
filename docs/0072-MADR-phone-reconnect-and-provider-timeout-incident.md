# MADR 0072: Phone reconnect failure, hung agent sessions, host config audit

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: **Accepted — decisions locked** (remediation not started).
  Owner 2026-08-05: §6.2 A; D1/D2/D4/D5/D6 yes; D3 write **20 s**; D7 serialize dials.
  Companion plan is the implementation authority.
- **Date**: 2026-08-05
- **Deciders**: Project Owner (priority / ops actions / D4); Implementer
  (code follow-ups after acceptance)
- **Implementation plan**:
  [0072-PLAN-phone-reconnect-and-provider-timeout-remediation.md](0072-PLAN-phone-reconnect-and-provider-timeout-remediation.md)
  — P0 ops through P6 close-out; grounded against HEAD+WIP
- **Scope**: Live incident on host `macos-laptop` (mcremote 0.8.0.2.g077c979)
  and phone `s22+`: mesh+relay disconnect / failed reconnect; goose and
  codex sessions that appear frozen until the app is force-killed; full
  audit of host `~/.config/mcremote/config.yaml` and provider timeout
  surfaces (goose, codex, opencode, grok); comparison to tree defaults and
  uncommitted WIP in the working tree
- **Related**:
  [0063](0063-MADR-connection-liveness-truth.md) (app-ping / LinkHealth),
  [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md)
  (resume, 4001, caps, dial episodes),
  [0058](0058-MADR-macos-launchd-service-hardening.md) (LaunchAgent lifecycle),
  [0065](0065-MADR-update-automation.md) (binary swap + service control),
  [0046](0046-MADR-reconnect-and-pairing-hardening.md) (park / permanent
  errors), [0071](0071-MADR-codebase-assessment.md) (prior software pass)
- **Method**: live host forensics (config, LaunchAgent, `devices.json`,
  `mcremote.err.log`, `launchctl`, Tailscale, binary version); code
  path review for liveness, write deadline, stall notices, transcript
  busy state, re-pair; matrix of host values vs `config.Defaults()` vs
  example templates vs uncommitted WIP

---

## 0. Executive summary

Three independent failure classes stacked on this host and produced the
symptoms the operator saw:

| Class | Symptom | Dominant cause (this host) |
| --- | --- | --- |
| **A — Daemon down** | Phone cannot reconnect to **mesh or relay** | LaunchAgent **bootout** at 17:00; service **not loaded**; no `mcremote` process; no mcrelay host registration |
| **B — Flaky while up** | Disconnect loops, tunnel storms, brief “connected then dead” | `reason=read_deadline` (214 historical) when the phone stops app-pinging; multi-dial relay tunnel storms; short WS **write** deadline (5 s on shipped binary) → `write_failed` / broken pipe |
| **C — Session looks hung** | Goose/codex “freeze” until app kill | Host pins **`providers.codex.turn_stall_notice_seconds: 0`**; lost `turn_complete` over WS blips leaves transcript `status=running`; re-pair orphan device ids + sticky composer busy |

**Right now (assessment time):** mesh and relay reconnect **must** fail
because **nothing is listening** — this is not a protocol mystery until
the daemon is running again.

**Shipped binary vs WIP:** installed `mcremote` is `077c979` (defaults:
WS read deadline **60 s**, codex stall **0**). The working tree already
contains uncommitted fixes moving those defaults to **120 s** and
raising the WS **write** deadline **5 s → 20 s** (D3), but they are **not
installed** and the **host config still pins codex stall to 0**, which
would defeat a new code default after deploy.

**Recommendation (order):**

1. **Ops P0** — rebootstrap LaunchAgent, fix host config, prune orphan
   devices, restart phone after single clean pair.
2. **Ship WIP defaults** — commit + install the read/write deadline and
   codex stall default changes already in the tree.
3. **Code follow-ups** — re-pair should replace/revoke prior device for
   the same client key; service control must never leave a permanent
   bootout after update/stop without heal; optional mobile “clear sticky
   running on resync when host says idle”.

---

## 1. Live host evidence (2026-08-05)

### 1.1 Process / launchd

| Check | Result |
| --- | --- |
| `pgrep` / `ps` for mcremote | **No process** |
| `launchctl print gui/$UID/com.magiccliremote.mcremote` | **Could not find service** |
| Plist on disk | Present: `~/Library/LaunchAgents/com.magiccliremote.mcremote.plist` (2026-08-01) |
| `print-disabled` | Label **enabled** (not Login-Items disabled) |
| Binary | `~/.local/bin/mcremote` → `0.8.0.2.g077c979` |
| Last log lines | `17:00:20` — `shutting down` / `terminated signal received`; then launchd `service inactive` + **`removing service`** |
| Prior restart same day | `12:55:38` shutdown → `12:55:50` start (brief outage) |

`KeepAlive=true` only restarts a **loaded** job after exit. **`launchctl
bootout` removes the job** from the domain; the next failure mode is
“plist exists, enabled, not running” until `bootstrap` / `setup-service
--force` / `service.Start`. That matches 0065’s `Stop` = bootout path
when a later `Start` does not run or fails silently from the operator’s
POV.

`com.magiccliremote.mcrelay` appears **disabled** in
`launchctl print-disabled` on this Mac. Relay **server** is expected on
the headscale host (`wss://headscale.lallygag.net:8443`); local mcrelay
agent disable is only relevant if this laptop was also meant to run a
relay. Host **registration** still requires a live **mcremote** with
`relay.url` set — which it is not, while down.

### 1.2 Config snapshot (redacted semantics)

Path: `~/.config/mcremote/config.yaml` (mtime 2026-08-04 21:52).

| Area | Host value | Code default (`Defaults()`, WIP tree) | Assessment |
| --- | --- | --- | --- |
| `listen` | `tailscale:7531` | same pattern | Sane |
| `tls` | selfsigned (empty mode, enabled) | OK for mesh IP advertise | OK; fingerprint must match phone pin |
| `auth.require_client_key` | true | product default | OK |
| `relay.url` | `wss://headscale.lallygag.net:8443` | empty = off | OK for phone path |
| `relay.host_id` | `macos-laptop` | — | OK |
| `limits.max_ws_clients` | 8 | 8 | OK |
| `limits.max_live_sessions` | 16 | 16 | OK |
| `limits.ws_read_deadline_seconds` | **absent** | **120** (WIP) / **60** (shipped 077c979) | **Drift**: host relies on binary default; shipped still 60 |
| `limits.ws_resume_window_seconds` | **absent** | 120 | Should be explicit |
| `limits.tcp_keepalive` | **absent** | 25/5/4 enabled | OK via default; document |
| `providers.grok` stall / prewarm / perm | 120 / true / 120 | 120 / true / 120 | **Sane** |
| `providers.goose` stall / prewarm / perm | 120 / **false** / 120 | 120 / false / 120 | **Sane** (cold first session) |
| `providers.opencode` stall / prewarm / perm | 120 / true / 120 | 120 / true / 120 | **Sane** |
| `providers.codex.turn_stall_notice_seconds` | **0** | **120** (WIP) / **0** (shipped + old templates) | **Misconfig — primary hang UX bug** |
| `providers.codex.permission_timeout_seconds` | 900 | 900 | Sane (long tools) |
| `providers.codex.prewarm` | false | false | OK |
| `providers.codex.allow_full_access` | **true** | false (templates) | Intentional per 0069; keep |
| `providers.codex.sandbox_broken_policy` | **absent** | empty / platform default | Add when 0048/0071 policy lands |

### 1.3 Disconnect forensics (`mcremote.err.log`)

Aggregate disconnect reasons (log lifetime sample):

| Reason | Count | Meaning |
| --- | --- | --- |
| `read_deadline` | **214** | Rolling authenticated read deadline expired (no client frames / pings) |
| `peer_closed` | 79 | Phone closed cleanly or mid-reconnect |
| `write_failed` | 9 | Host write timed out / broken pipe to peer |

Notable episodes:

- **10:23–10:26** — multiple `device authenticated` within ~2 min for
  `a752a705…`, mix of tunnel + direct, ends in `read_deadline`.
- **11:46–11:47** — **relay tunnel storm**: ≥15 `tunnel bridged` events
  in ~70 s (each a dial episode join). Classic multi-dial / park-resume
  overlap under failure.
- **12:55** — daemon restart (signal); sessions drop.
- **12:56–12:57** — phone reauth `a752a705…`, then `read_deadline` after
  ~60 s (matches **shipped 60 s** deadline with no effective app-ping).
- **13:33** — **re-pair** short code → new `device_id=51090d1c…` same
  name `s22+`, **same `client_key_fp`** as `a752a705…`.
- **17:00** — daemon terminated; launchd **removed** the service; log
  silent since.

### 1.4 Pairing store hygiene

`mcremote pair list`: **24** devices, all named `s22+`.

| Stat | Value |
| --- | --- |
| Unique client key fingerprints | 14 |
| Never used (`last_used` empty) | 9 |
| Active key `HCr_AgEKryIg…` | **two** device ids: `a752a705…` (last used today) and `51090d1c…` (re-pair, no last_used yet in list snapshot timing) |

`mcremote pair prune --stale <d>` / `--keyless` exist and should be used;
re-pair does **not** auto-revoke the prior id for the same key (see F4).

Disk sessions remain owned by many historical device ids (mostly
`status=disconnected`). Not fatal after daemon death, but amplifies
“stale session” confusion after re-pair.

### 1.5 Network

Tailscale peers present (`100.64.0.3` host, `100.64.0.2` phone). Mesh
routing is available; **application listener is not**.

---

## 2. What is *not* a finding (checked)

| Candidate | Why rejected |
| --- | --- |
| “Relay config wrong on host” | `relay.url` + `host_id` set; registration succeeded whenever daemon was up |
| Goose stall disabled | Host goose stall is **120** — not the hang culprit |
| Opencode / grok timeout misconfig | Host matches sane defaults (stall 120, grok/opencode prewarm true) |
| Need mcrelay LaunchAgent on laptop | Product path is **outbound register** to remote mcrelay; local mcrelay agent is optional |
| Phone must use only mesh | 0062 dual-path is correct; both fail when host daemon is down |
| Transcript cache restoring `running` | Cache **forces** cached `running` → `idle` on load (`transcript_cache.dart`) — already fixed for process restart |

---

## 3. Findings

Severity: **S0** common-path break / data-loss class · **S1** user-visible
wrong behaviour · **S2** incomplete product wiring · **S3** harden/docs.

### F1 — LaunchAgent bootout leaves daemon permanently down (S0)

**Evidence:** 17:00 `terminated signal` + launchd `removing service`;
plist remains; `print` cannot find job; no process.

**Why it matters:** Phone mesh (`wss://100.64.0.3:7531`) and relay
(host registration) both depend on a live daemon. Operator sees
“cannot reconnect to either path” which is true and total.

**Likely mechanisms:**

1. `internal/cli/service.Stop` = `launchctl bootout` (removes job).
2. 0065 `SwapAndRestart` stops then starts; if Start fails or
   RestartService/HealStart not applied for a particular CLI path, job
   stays out.
3. Manual `bootout` / incomplete `setup-service` without re-bootstrap.

**Remediation:**

- **Ops now:** `mcremote setup-service --force` (or bootstrap +
  kickstart the existing plist).
- **Code:** ensure every update/stop path that bootouts always
  re-bootstraps when product should stay up; surface loud CLI errors;
  consider `doctor` check “plist present + enabled + not loaded”.
- **Docs:** never document bootout alone as “restart”.

### F2 — Host pins codex `turn_stall_notice_seconds: 0` (S1)

**Where:** `~/.config/mcremote/config.yaml`; historical templates
shipped 0 (WIP flips templates/defaults to 120).

**What:** Stall notices are the only structured “still working, no
output” signal during multi-minute codex tools (`flutter test`, builds).
With 0, a WS blip that drops `turn_complete` leaves the phone composer
in **busy/running** with no stall banner and no end event — feels
frozen. Force-killing the app clears in-memory transcript state (and
cache already downgrades stored `running` → `idle`).

**Goose:** host stall is 120 — less likely the same config bug; hangs
still possible from F3/F5 transport loss + busy sticky.

**Remediation:** set host codex stall to **120** (or delete the key
after shipping new defaults). Align all example/setup templates (WIP
already does).

### F3 — Read-deadline disconnects dominate; phone often stops pinging (S1)

**Evidence:** 214× `read_deadline`; 12:56 auth → 12:57 disconnect ≈ 60 s
on shipped binary.

**Mechanism (0063/0068):** authenticated connection has a rolling read
deadline reset by inbound frames (and, when negotiated, app pings). If
the phone is backgrounded, Doze-deferred, frozen UI-isolate, or parked
without clean close, the host reaps. Phone may still believe it is
connected until LinkHealth/ping logic fires — or may enter multi-dial
storms (F5).

**Shipped default 60 s** is tight for brief mesh blips + deferred
timers. **WIP default 120 s** is better; TCP keepalive (~45 s reap) still
kills true blackholes first when enabled.

**Remediation:** ship 120 s default; set explicit
`limits.ws_read_deadline_seconds: 120` on host; keep app-ping 10 s when
foreground; hardware-validate background park policy (0068/0063 Part A).

### F4 — Re-pair allocates a new device id without revoking the prior key twin (S1)

**Evidence:** `a752a705…` and `51090d1c…` share `client_key_fp`; 24
historical `s22+` rows; sessions owned by many dead ids.

**What:** Short-code re-pair creates a **new** device record. Old token
row remains valid until prune/revoke. Capacity, debugging, and
session-owner filters get noisy; dual-id confusion during incident
response is real.

**Remediation:**

- **Ops:** `mcremote pair prune --stale 72h` (tune) and/or revoke all but
  the active id; optional one-shot cleanup of never-used rows.
- **Code:** on successful pair.claim with an enrolled client key already
  bound to another device, **revoke or merge** the previous device id
  (product decision — prefer single active device per key unless
  multi-device is intentional).

### F5 — Relay tunnel / multi-dial storms under reconnect pressure (S1)

**Evidence:** 11:46 ≥15 tunnel bridges in ~70 s; earlier multi-auth
bursts at 10:23–10:26.

**What:** Each failed or overlapping dial episode can open a new relay
join before the previous is torn down. Host still authenticates; load
and races increase `write_failed` / replacement pressure. 0068 work
reduced some park/resume races; production logs show residual storms.

**Remediation:** tighten single-flight dial (episode) guarantees under
relay failover; consider host-side rate limit / coalesce of joins per
device; ops: avoid thrashing reconnect while daemon is mid-restart.

### F6 — WS write deadline 5 s on shipped binary (S1 → mitigated in WIP)

**Where:** `internal/ws/server.go` `writeDeadline` — **5 s** at 077c979;
Target is **20 s** (owner D3; WIP was 15 s — align to 20 before ship).

**Evidence:** `write_failed` with `broken pipe` in logs during reconnect
bursts.

**Remediation:** ship **20 s** write deadline (D3).

### F7 — Host limits omit explicit v2 liveness keys (S2)

**What:** Only `max_ws_clients` / `max_live_sessions` are written. After
defaults change, operators cannot see the contract they run. Resume
window and keepalive are invisible.

**Remediation:** write explicit keys in host config and setup template
(`defaults_mcremote.yaml` WIP already adds read/resume).

### F8 — Sticky `running` after lost terminal events (S1, product)

**Where:** `applyMetaStatus` only clears busy when host meta moves out
of `running`; live path depends on `turn_complete` / status events.

**What:** Dropped frames during F3/F5 leave the chat composer in queue/
stop mode until a later host status or app restart. Cache load already
clears stored `running`; **live memory** does not.

**Remediation:** on SessionSynchronizer resync, if host snapshot status
is `idle`/`disconnected`, force-clear local `running` and pending
permissions; optional “Reset turn” UI; stall notices (F2) reduce
perceived freeze even when still running for real.

### F9 — Grok prewarm warns about unconfigured auth methods (S3)

**Evidence:** `agent advertises auth methods but none configured;
session/new may fail` on every prewarm.

**What:** Sessions still created successfully today; warn is noise and
may hide real auth failures later.

**Remediation:** set `providers.grok.auth_method_id` if required by the
installed grok CLI, or silence when sessions work without it.

### F10 — Uncommitted timeout/relay WIP not installed (S2 process)

**Working tree (not in 077c979 binary):**

- Codex stall default 0 → 120; templates updated.
- WS read deadline default 60 → 120; tests updated.
- WS write deadline 5 → **20 s** (D3).
- Relay TLS handshake ErrorLog demotion (scanner noise).

Until commit + `make install` + service restart, the host cannot benefit
even with config edits for keys that only exist in new binaries.

---

## 4. Provider configuration matrix (sane targets)

| Key | grok | goose | opencode | codex |
| --- | --- | --- | --- | --- |
| `enabled` | true | true | true | true |
| `permission_timeout_seconds` | 120 | 120 | 120 | **900** |
| `prewarm` | **true** | false | **true** | false |
| `turn_stall_notice_seconds` | **120** | **120** | **120** | **120** (never 0 in production phones) |
| `stream_coalesce_ms` | 80 | 80 | 80 | 80 |
| `allow_full_access` | n/a | n/a | n/a | true only if operator accepts full-access mode (this host: true) |
| `sandbox_broken_policy` | n/a | n/a | n/a | set once 0048 policy is chosen (`fail` / `warn` / …) |

| Limits key | Sane value | Notes |
| --- | --- | --- |
| `max_ws_clients` | 8 | OK |
| `max_live_sessions` | 16 | OK |
| `ws_read_deadline_seconds` | **120** | Floor 15; advertise in v2 caps |
| `ws_resume_window_seconds` | **120** | Match resume token TTL |
| `tcp_keepalive` | default 25/5/4 | Keep enabled; reap ~45 s |

Goose-specific notes:

- `prewarm: false` is fine; first session pays `goose serve` cold start —
  not a hang, but can look like one if stall is misread. Stall 120 helps.
- Long agentic turns still need F3/F8 transport + sticky-busy fixes.

Codex-specific notes:

- Permission timeout 900 is intentional for long sandbox tool runs.
- Stall **must not** stay 0 on phone-facing hosts.
- `allow_full_access: true` is an operator security choice (0069), not a
  reconnect bug.

---

## 5. Root-cause map (symptom → fix)

```text
Phone cannot reconnect mesh OR relay
  ├─ Daemon not running (F1) ────────── setup-service / Start heal
  ├─ Daemon up but read_deadline (F3) ─ 120s deadline + app-ping health
  ├─ Multi-dial tunnel storm (F5) ───── single-flight dial + ship 20s write
  └─ Stale pair / dual device ids (F4) ─ prune + re-pair once cleanly

Goose/codex look frozen until app kill
  ├─ codex stall 0 (F2) ─────────────── set 120 in host config
  ├─ lost turn_complete (F3/F8) ─────── resync clears running; stall UX
  ├─ daemon restart mid-turn (F1) ───── KeepAlive + no orphan bootout
  └─ pending permission timeout ─────── 120s goose/grok; 900s codex OK
```

---

## 6. Recommended actions (checklist for review)

### 6.1 Immediate ops (this host) — no code required

1. **Restore daemon**
   - `mcremote setup-service --force`
   - Verify: `launchctl print gui/$(id -u)/com.magiccliremote.mcremote`
     shows `state = running`
   - Verify log: `listening` + `registered with mcrelay`
2. **Edit `~/.config/mcremote/config.yaml`**
   - `providers.codex.turn_stall_notice_seconds: 120`
   - Under `limits:` add:
     - `ws_read_deadline_seconds: 120`
     - `ws_resume_window_seconds: 120`
   - Optionally document `tcp_keepalive` block if operators will tune it
3. **Restart after config** — kickstart LaunchAgent
4. **Pairing cleanup**
   - Identify active device (current phone token)
   - `mcremote pair prune --stale 48h` (or revoke named orphans)
   - Prefer **one** device id for `s22+`
5. **Phone** — force-stop app once after host is healthy; single connect;
   confirm LinkHealth fresh and one transport (mesh or relay)

### 6.2 Software to ship (accept this MADR → implement)

| Priority | Item | Finding |
| --- | --- | --- |
| P0 | Commit + install WIP: codex stall default 120, read deadline 120, write deadline **20 s**, template/docs | F2, F3, F6, F10 |
| P0 | Service heal: never leave bootout without Start when product should run; doctor “enabled but not loaded” | F1 |
| P1 | Re-pair replaces prior device for same client key (or auto-prune twins) | F4 |
| P1 | SessionSynchronizer / transcript: host idle clears local running + pendings | F8 |
| P2 | Dial single-flight / join rate limit under relay | F5 |
| P2 | Explicit limits keys in setup-written config | F7 |
| P3 | Grok auth_method_id warning cleanup | F9 |

### 6.3 Explicit non-goals of this MADR

- Replacing 0065 update automation design
- Re-opening full 0068 protocol design (only residual storm + sticky busy)
- Changing goose dangerous-auto policy (0069 closed)

---

## 7. Decision drivers

- **Operator time:** a dead LaunchAgent is a five-minute fix and explains
  total reconnect failure better than any protocol theory.
- **Config truth over code defaults:** host YAML currently **overrides**
  the most important UX knob (codex stall) to off.
- **Ship what is already written:** WIP in the tree matches the forensics;
  leaving it uninstalled prolongs the incident class.
- **Re-pair hygiene:** 24 devices for one phone is an ops hazard and a
  product gap.

---

## 8. Decisions (for owner acceptance)

| ID | Proposal | Default if silent |
| --- | --- | --- |
| **D1** | Production hosts must set codex `turn_stall_notice_seconds` ≥ 60 (recommend 120); templates/default 120 | **Locked yes** |
| **D2** | Default `ws_read_deadline_seconds` is 120; keep TCP keepalive ~45 s blackhole reap | **Locked yes** |
| **D3** | Default WS write frame deadline | **Locked 20 s** |
| **D4** | Successful pair.claim with existing client key **revokes** previous device ids for that key (single active device per key) | **Locked yes** |
| **D5** | `service.Stop` bootout must be paired with documented Start; update heal-on-down stays default for install paths | **Locked yes** (HealStart on update) |
| **D6** | Host config should materialize explicit limits liveness keys at setup-service time | **Locked yes** |
| **D7** | Serialize DialEpisodes for reconnect robustness | **Locked yes** (implement P5) |
| **§6.2** | Resume fast path still lists + syncFromMeta; skip history only (option A) | **Locked** |

---

## 9. Consequences

### Positive

- Clear split between **ops outage** (daemon down) and **protocol UX**
  (stall, sticky busy, storms).
- Actionable host reconfig that does not wait on a large rewrite.
- Aligns examples, defaults, and this host’s lived failure modes.

### Negative / residual

- 120 s read deadline delays host reclaim of truly dead peers if
  keepalive is disabled (keepalive remains on by default).
- Stall notices can fire during legitimate quiet tool runs (120 s is a
  compromise; 0 is worse for phones).
- D4 may surprise if two physical devices share one client key store
  (unlikely in current product).

### Risks if we do nothing

- Next binary swap or bootout leaves the phone “broken” again with no
  app-side fix.
- Codex multi-minute tools continue to look frozen after any blip.
- Pairing store and session ownership keep growing noise.

---

## 10. Validation plan (after remediation)

1. `launchctl print` shows running; log shows mcrelay register.
2. Phone mesh connect stable ≥10 min foreground; LinkHealth fresh.
3. Background 2 min → foreground reconnect without app kill (Part A row).
4. Kill `-TERM` LaunchAgent job — KeepAlive or heal brings it back;
   bootout path only via explicit stop with Start after updates.
5. Codex turn running `sleep 180` or long test: stall notice by ~120 s;
   turn_complete clears busy without app restart.
6. `pair list` shows ≤1–2 devices for the phone after prune.
7. Induce relay-only path: no tunnel storm (≤1 join per dial episode).

---

## 11. Appendix — host config edit sketch

```yaml
providers:
  codex:
    turn_stall_notice_seconds: 120   # was 0
    # keep permission_timeout_seconds: 900
    # keep allow_full_access: true if still desired

limits:
  max_ws_clients: 8
  max_live_sessions: 16
  ws_read_deadline_seconds: 120
  ws_resume_window_seconds: 120
  # optional explicit keepalive:
  # tcp_keepalive:
  #   enabled: true
  #   idle_seconds: 25
  #   interval_seconds: 5
  #   count: 4
```

After edit: kickstart service; confirm log line advertising / using 120 s
deadline once the new binary is installed.

---

## 12. Open questions

1. **D4 multi-device:** should one client key ever map to multiple device
   ids (tablet + phone), or is one-key-one-device the product rule?
2. What exactly invoked the 17:00 bootout (update CLI, manual stop,
   install script)? Confirm via shell history / 0065 path so heal covers
   that entry point.
3. Should goose `permission_timeout_seconds` rise above 120 for parity
   with long remote approvals, or is 120 correct for goose UX?

---

## 13. Planning notes (post code-grounding)

When preparing the companion plan, these MADR claims were refined:

| Item | Refinement |
| --- | --- |
| F8 sticky busy | `applyMetaStatus` already clears `running` when list meta is non-running. Residual bug: **resume fast path** (`coveredAndUnchanged`) skips `listSessionSnapshot` / `syncFromMeta` entirely, so status never reconciles when seq matches. Plan P4 fixes that, not a greenfield status engine. |
| F1 update-only | `update.Run` never sets `HealStart` (heal tested but unwired). 17:00 bootout is consistent with any Stop without Start; plan heals update + doctor, not only “blame update”. |
| Host stall 0 after ship | `setup-service` **never overwrites** existing config — P0 host edit remains mandatory even after P1 defaults land. |

---

## 14. Document history

| Date | Note |
| --- | --- |
| 2026-08-05 | Initial incident MADR for review; grounded in live host forensics + tree/WIP audit |
| 2026-08-05 | Linked 0072-PLAN; added §13 planning refinements from code grounding |
| 2026-08-05 | Owner locked §6.2 A; D1–D7 (D3 write **20 s**) |
