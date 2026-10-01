---
status: draft
date: 2026-08-18
madr: "0099-MADR-installer-service-state-verification.md"
owner: Project Owner
target: v0.13.5
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Verify service state before reporting it, and repair the s6 and relay backends

Associated MADR:
[0099-MADR-installer-service-state-verification.md](0099-MADR-installer-service-state-verification.md)

## Objective and Scope

Make `install.sh` report only what it has measured, then repair the three
mechanisms the [0098 sweep](0098-findings-install-verification-sweep.md) proved
broken, and re-prove each fix on the same real hosts that found it.

**Done means:** every ✅ row in the 0099 §Confirmation table has been re-run
against a released artifact and passes; `scripts/install_test.sh` carries a
regression assertion that fails on today's code; and no summary line in
`install.sh` asserts a capability that was not probed.

**In scope:** `scripts/install.sh`; the `mcrelay` unit template and its default
config; the Go tests that pin those; `scripts/install_test.sh`; `0097-PLAN`
§4.G and `ops-linux-install.md` where the summary vocabulary is documented.

**Out of scope, deliberately:**

* **`--with-relay-service` on non-systemd backends.** `svc_runit`, `svc_s6` and
  `svc_openrc` write a unit for `mcremote` only, so the flag is silently ignored
  there. Real, but distinct from F4 and never exercised by the sweep — it needs
  measurement before a fix.
* **`mcrelay`'s TLS design.** This plan makes `setup-service` emit a config that
  *starts*. What a relay should bind by default in production belongs to
  MADR 0091's owner.
* **The `sysvinit` classification.** Row 8b was blocked (0098), so there is no
  measurement to decide on.
* **macOS/launchd.** `plist_render.go` has its own template and no reported
  defect; touching it would widen the blast radius for no evidence.

## Prerequisites and Dependencies

### Defect locations, verified in the working tree 2026-08-18

| Ref | File | Line(s) | Current content |
|---|---|---|---|
| F1 | `scripts/install.sh` | 221 | `s6) s6-svc -l "$S6_DIR/$1" >/dev/null 2>&1 ;;` |
| F5 | `scripts/install.sh` | 331, 306 | `SERVICE_RESULT="supervised+boot"` set on a zero exit |
| F5 | `scripts/install.sh` | 445 | `summary()` prints the unverified claim |
| F6 | `scripts/install.sh` | 413 | `systemd-broken` `su`/`pam_systemd` advisory |
| F4a | `internal/cli/service/mcrelay.user.service.tmpl` | 65, 66 | `PrivateDevices=true`, `RestrictNamespaces=true` |
| F4a | `internal/cli/service/mcrelay.user.service.tmpl` | 71–74 | the exclusion-list comment these two belong on |
| F4b | `internal/cli/service/defaults_mcrelay.yaml` | 10–17 | `host: "0.0.0.0"`, `port: 8443`, `tls.mode: ""` |
| F4b | `internal/relay/fileconfig.go` | 177 | `Listen: ListenConfig{Host: "0.0.0.0", Port: 8443}` |
| F4b | `internal/relay/server.go` | 179 | the refusal this default triggers |

### Tests that will fail on purpose and must be updated in the same commit

| File | Symbol | Line | Why it breaks |
|---|---|---|---|
| `internal/cli/service/setup_test.go` | `TestRenderUnitMcrelay` | 98 | asserts `PrivateDevices=true` is **present** (and `RestrictNamespaces=true` alongside it) |
| `internal/cli/service/setup_test.go` | `TestSetupWritesDefaultMcrelayConfig` | 180 | a **second, separate** `PrivateDevices=true` assertion — `t.Fatalf`, so it aborts the test rather than just failing |
| `internal/cli/service/setup_test.go` | `TestRenderUnitMcremote` | 59 | asserts they are **absent** — must keep passing unchanged |

Three assertion sites, not two. `TestSetupWritesDefaultMcrelayConfig` is easy to
miss because its name suggests it only covers the config file; it happens to
assert on the unit body as well. Grep before editing:

```sh
grep -n 'PrivateDevices\|RestrictNamespaces' internal/cli/service/*.go
```

`TestRenderUnitMcremote`'s negative assertions are the reason F4a is safe: they
prove `mcremote` never carried these directives, so removing them from `mcrelay`
converges the two templates rather than diverging them.

### Environment

* Go toolchain for the unit tests and a release build.
* `shellcheck` (`-s sh`, not bash).
* For §Verification: the 0098 harness — `aws` authenticated, `us-east-1`,
  subnet `subnet-0fc17839ef9f6d906`, and **an RSA key pair** for the Windows
  host (0098 F7: EC2 rejects ed25519 for Windows AMIs at launch).

### Blocking dependencies

**Phase 5 cannot start until a release carrying Phases 1–4 exists.** The sweep
installs from `releases/latest/download/`, not from the working tree — a fix in
the tree is unverifiable by the harness that found the defect.

## Technical Design

### The verification gate (F5)

`SERVICE_RESULT` stops being an assumption and becomes an observation. One new
function, one new call site, three new summary branches.

**Why a naive `is-active` check is not enough.** Both measured failures presented
as `ActiveState=activating`, **not** `failed` — because `Restart=always` puts a
crash-looping unit into `auto-restart` forever. A gate that waits for `failed`
would wait for a state that never arrives. The measured evidence:

```text
F4  (relay)        ActiveState=activating SubState=auto-restart MainPID=0 NRestarts=6
F5  (port clash)   ActiveState=activating SubState=auto-restart MainPID=0 NRestarts=3
```

So the gate distinguishes *slow* from *looping* using `NRestarts`, which is the
only field that separates them:

```sh
# How long to wait for a service to settle. Generous: a cold host under load
# can legitimately take several seconds. Overriding it is a debugging aid, not
# a documented flag.
SVC_WAIT_SECS="${MCREMOTE_SVC_WAIT_SECS:-15}"

# svc_settle <product>
#   0 = confirmed running
#   1 = still coming up when the window expired (slow, not necessarily broken)
#   2 = confirmed not running (failed, or restart-looping)
svc_settle() {
    _p=$1; _i=0
    while [ "$_i" -lt "$SVC_WAIT_SECS" ]; do
        case "$INIT" in
            systemd-user)
                _as=$(systemctl --user show "$_p" --property=ActiveState --value 2>/dev/null || echo unknown)
                _ss=$(systemctl --user show "$_p" --property=SubState    --value 2>/dev/null || echo unknown)
                _nr=$(systemctl --user show "$_p" --property=NRestarts   --value 2>/dev/null || echo 0)
                [ "$_as" = active ] && return 0
                [ "$_as" = failed ] && return 2
                # Restart=always never reaches `failed`; a unit that has already
                # restarted twice inside the window is looping, not starting.
                if [ "$_ss" = auto-restart ] && [ "${_nr:-0}" -ge 2 ]; then
                    return 2
                fi
                ;;
            *)  svc_is_active "$_p" && return 0 ;;
        esac
        _i=$((_i+1)); sleep 1
    done
    return 1
}
```

Backends with no meaningful liveness probe (`none`, `systemd-broken`,
`openrc-system`) never reach the gate: they already report `none` and are
untouched.

**Reporting and exit codes.** `starting` is deliberately **not** a failure —
converting a slow-but-healthy host into a red error would be a new defect in the
opposite direction (0099 §Consequences).

| `svc_settle` | `SERVICE_RESULT` | Exit | Summary line |
|---|---|---|---|
| 0 | unchanged (`supervised+boot` / `supervised` / `supervised-session`) | 0 | as today |
| 1 | `starting` | **0** | `service:  starting — not yet confirmed running` + check command |
| 2 | `failed` | **3** | `service:  FAILED to start — binaries are installed` + backend diagnostic |

On `failed`, print the backend's own diagnostic rather than a generic message,
because the diagnostic is what makes the report actionable:

```sh
svc_diagnose() { # $1 = product
    case "$INIT" in
        systemd-user)
            log "  journalctl --user -u $1 -n 20 --no-pager"
            journalctl --user -u "$1" -n 8 --no-pager 2>/dev/null | sed 's/^/  | /' >&2 || true ;;
        s6)    log "  s6-svstat $S6_DIR/$1" ;;
        runit) log "  SVDIR=$RUNIT_DIR sv status $1" ;;
    esac
}
```

**Call site.** In `setup_service()`, replacing the bare
`SERVICE_RESULT="supervised+boot"` at `:331` and the upgrade-path equivalent at
`:306`:

```sh
    _claimed=$SERVICE_RESULT          # what the backend intended to deliver
    svc_settle mcremote; _rc=$?
    case "$_rc" in
        0) SERVICE_RESULT=$_claimed ;;
        1) SERVICE_RESULT=starting ;;
        2) SERVICE_RESULT=failed; svc_diagnose mcremote; return 1 ;;
    esac
```

`--with-relay-service` gates `mcrelay` the same way; a relay that fails to
settle downgrades the overall result but must not discard a healthy `mcremote`.

### F1 — the s6 liveness probe

`s6-svc -l` is not a valid option; it exits **100** with a usage error, so the
probe is false on every s6 host. Measured on the same host, `s6-svstat` works:

```text
$ s6-svc -l ~/.local/share/s6/service/mcremote
s6-svc: usage: s6-svc [ -wu | -wU | ... ] servicedir      (exit 100)

$ s6-svstat ~/.local/share/s6/service/mcremote
up (pid 2515 pgid 2515) 40 seconds
```

```diff
 svc_is_active() { # $1 = product
     case "$INIT" in
         systemd-user) systemctl --user is-active "$1" >/dev/null 2>&1 ;;
         runit)        SVDIR="$RUNIT_DIR" sv status "$1" >/dev/null 2>&1 ;;
-        s6)           s6-svc -l "$S6_DIR/$1" >/dev/null 2>&1 ;;
+        # s6-svc has no -l; it exits 100 on a usage error, which made this
+        # probe permanently false and stopped uninstall/upgrade from ever
+        # cycling the daemon (MADR 0099 F1). s6-svstat is the status tool.
+        s6)           s6-svstat "$S6_DIR/$1" 2>/dev/null | grep -q '^up ' ;;
         openrc-user)  rc-service --user "$1" status >/dev/null 2>&1 ;;
         *) return 1 ;;
     esac
 }
```

Matching on `^up ` rather than exit status matters: `s6-svstat` exits 0 for a
*down* service too, so a bare exit check would swap a permanently-false probe
for a permanently-true one.

**Preflight consequence.** `s6-svstat` becomes required for the s6 backend. The
INIT probe currently tests `s6-svscan` and `s6-svc`; add `s6-svstat` so a host
with a partial s6 install degrades to `none` rather than to a broken s6 path.

### F4a — relay unit hardening directives

The template's own comment already names the failure mode and lists the
directives excluded for it. `PrivateDevices` (implies
`CapabilityBoundingSet=~CAP_MKNOD`) and `RestrictNamespaces` belong on that
list; the sweep proved both are required to clear `218/CAPABILITIES` and that
neither alone suffices.

```diff
-# mcrelay does not exec coding CLIs, so these are safe here (they are
-# not on the mcremote unit). Disable in a drop-in if a host cannot apply them.
-PrivateDevices=true
-RestrictNamespaces=true
 # 0091 D4: probed 2026-08-15 — bind + /healthz + register/join/splice
 # succeeded under both of these on a systemd --user unit (CGO_ENABLED=0).
 RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
 MemoryDenyWriteExecute=true
-# ProtectClock/ProtectKernelModules/ProtectKernelLogs are deliberately absent:
+# PrivateDevices/RestrictNamespaces/ProtectClock/ProtectKernelModules/
+# ProtectKernelLogs are deliberately absent:
 # they imply capability-bounding-set changes a user manager cannot perform in
 # container-restricted hosts (218/CAPABILITIES crash loop). ProtectHostname
 # needs a UTS namespace, also commonly prohibited there.
+# MADR 0099 F4a: PrivateDevices+RestrictNamespaces were on this unit until
+# v0.13.4 and made it unstartable in user scope on EVERY host — measured on
+# stock Ubuntu 26.04, not only container-restricted ones. Removing both was
+# necessary and sufficient; neither alone cleared it.
```

Note the correction the comment must carry: 0091 D4 assumed this class of
failure was specific to *container-restricted hosts*. It is not — a stock
Ubuntu 26.04 EC2 instance reproduced it. Leaving the old wording would preserve
the belief that led to shipping the directives.

### F4b — a default relay config that starts

`setup-service` writes `listen.host: 0.0.0.0`, `port: 8443`, `tls.mode: ""`,
and `internal/relay/server.go:179` then refuses exactly that combination. The
installer cannot know the operator's TLS intent, so the default must be the one
combination that is both safe and startable: **loopback**.

```diff
 listen:
-  host: "0.0.0.0"            # MCRELAY_LISTEN_HOST / --listen-host
+  # Loopback by default so a freshly provisioned relay STARTS. A public bind
+  # with tls.mode unset is refused by the daemon (internal/relay/server.go),
+  # which made `--with-relay-service` produce a crash-looping unit on every
+  # host until v0.13.4 (MADR 0099 F4b). Set a public host together with
+  # tls.mode=letsencrypt|files when you are ready to expose it.
+  host: "127.0.0.1"          # MCRELAY_LISTEN_HOST / --listen-host
   port: 8443                 # MCRELAY_LISTEN_PORT / --listen-port (443 for public LE)
```

Leave `internal/relay/fileconfig.go:177` alone: that is the *code* default for a
relay run without a config file, where an explicit operator choice is implied.
Only the **provisioning template** changes. Record this asymmetry in the commit
message so it is not "tidied up" later.

The `setup-service` output already prints the `setcap` note for ports < 1024;
extend it to say the shipped default is loopback and what to change for a public
bind.

### F6 — do not diagnose `su` on WSL

Measured on WSL2 with `systemd=false`: `XDG_RUNTIME_DIR` is
`/mnt/wslg/runtime-dir`, i.e. **set**, so the advisory's stated cause is
factually false on the host it is shown to.

```diff
-    if [ "$INIT" = systemd-broken ]; then
+    # Not on WSL: there the cause is the distro's systemd boot setting, and the
+    # su/pam_systemd story is wrong on every count — no su involved, and
+    # XDG_RUNTIME_DIR is set (/mnt/wslg/runtime-dir). MADR 0099 F6.
+    if [ "$INIT" = systemd-broken ] && [ "$ENVIRONMENT" != wsl1 ] && [ "$ENVIRONMENT" != wsl2 ]; then
```

The WSL2 and WSL1 advisories at `:421`/`:429` already fire on their own and are
correct; this only removes the misleading paragraph that preceded them.

## Execution Phases

Phases 1–4 are independently landable and each keeps the tree shippable.
**Phase 1 lands first**: without the gate, Phases 2–4 cannot be shown to have
worked from the installer's own output, because a broken service still reports
`running`.

---

### Phase 1 — The verification gate (F5)

**Deliverable:** `install.sh` never prints an unmeasured capability claim.

1. Add `SVC_WAIT_SECS`, `svc_settle()`, `svc_diagnose()` per §Technical Design.
2. Replace the two bare `SERVICE_RESULT="supervised+boot"` assignments
   (`:306`, `:331`) with the gated form.
3. Gate `mcrelay` too when `--with-relay-service` is set.
4. Add `starting` and `failed` branches to `summary()` (`:445`).
5. Reconcile exit codes: `starting` → 0, `failed` → 3.

**Exit criterion:** on a host where the unit cannot start, `install.sh` exits 3
and prints `FAILED to start` plus journal lines; on a healthy host output is
byte-identical to v0.13.4 apart from up to `SVC_WAIT_SECS` of latency.

---

### Phase 2 — s6 liveness probe (F1)

**Deliverable:** uninstall and upgrade actually stop an s6-supervised daemon.

1. Apply the `svc_is_active` diff.
2. Add `s6-svstat` to the s6 arm of `detect_init` so a partial s6 install
   degrades to `none`.
3. `shellcheck -s sh scripts/install.sh` clean.

**Exit criterion:** on an s6 host, `--uninstall` leaves no `mcremote` process
and no `/proc/*/exe` reporting `(deleted)`.

---

### Phase 3 — Relay unit template (F4a)

**Deliverable:** `mcrelay.service` starts in user scope.

1. Remove both directives; rewrite the exclusion comment, including the
   correction that this is **not** container-specific.
2. Update **all three** assertion sites in
   `internal/cli/service/setup_test.go` (grep first — see §Prerequisites):
   move `PrivateDevices=true` / `RestrictNamespaces=true` out of the `want`
   list in `TestRenderUnitMcrelay` (:98), and delete the standalone
   `PrivateDevices` check in `TestSetupWritesDefaultMcrelayConfig` (:180).
   Add both directives to a **negative** assertion on the mcrelay unit so the
   regression cannot return.
3. Leave `TestRenderUnitMcremote` untouched; it must still pass.
4. `go test ./internal/cli/service/...` green.

**Exit criterion:** rendered `mcrelay` unit contains neither directive; both
render tests pass; a manual `systemd-analyze verify` (if available) is clean.

---

### Phase 4 — Relay default config (F4b)

**Deliverable:** the config `setup-service` writes produces a relay that starts.

1. Change `defaults_mcrelay.yaml` `listen.host` to `127.0.0.1` with the
   explanatory comment.
2. Leave `internal/relay/fileconfig.go:177` unchanged; note why in the commit.
3. Extend the `setup-service` public-ports note to state the shipped default.
4. Check `template_parity_test.go` still passes — it compares provider keys
   between template and example config, and a `listen.host` change may be in
   scope depending on what it walks. If it fails, update the example config in
   the same commit rather than weakening the test.

**Exit criterion:** `mcrelay serve --config <freshly written config>` starts and
serves `/healthz` on loopback.

---

### Phase 5 — WSL advisory (F6)

**Deliverable:** WSL hosts get only the advisory that applies.

1. Apply the `advisories()` guard.
2. Add harness cases 15–16 (below).

**Exit criterion:** with `ENVIRONMENT=wsl2` and `INIT=systemd-broken`, output
contains the wsl.conf remedy and not the string `pam_systemd`.

---

### Phase 6 — Harness and documentation

**Deliverable:** the contract is a test, and the docs match the vocabulary.

New cases in `scripts/install_test.sh`, using the existing
`ok`/`bad`/`check`/`contains` helpers and the `mk_stubs`/`probe_init` pattern:

| # | Case | Assertion |
|---|---|---|
| 15 | **contract guard** — stub backend whose liveness probe reports not-running | summary never contains `supervised+boot`; exit 3. **This fails on today's code** |
| 16 | slow start — probe reports not-active for the whole window | `SERVICE_RESULT=starting`, exit **0** |
| 17 | crash loop — stub `systemctl` returns `activating`/`auto-restart`/`NRestarts=6` | `SERVICE_RESULT=failed`, exit 3 |
| 18 | s6 probe — stub `s6-svstat` printing `up (pid 1) 3 seconds` | service treated as active |
| 19 | s6 probe — stub `s6-svstat` printing `down 0 seconds` | service treated as **not** active |
| 20 | s6 partial install — `s6-svscan` present, `s6-svstat` absent | `INIT=none` |
| 21 | WSL2 + `systemd-broken` | output has wsl.conf remedy, **not** `pam_systemd` |
| 22 | native + `systemd-broken` | output **does** have the `pam_systemd` paragraph |

Set `MCREMOTE_SVC_WAIT_SECS=1` in the harness so cases 16–17 do not add 15
seconds each to the suite.

Documentation, in the same commit:

* `0097-PLAN` §4.G — add `starting` and `failed` to the capability/vocabulary
  table.
* `ops-linux-install.md` — same vocabulary; correct the s6 control command to
  `s6-svstat`; note the relay default is loopback.

**Exit criterion:** suite reports ≥ 65 assertions, all passing; case 15 verified
to fail when run against the v0.13.4 script.

---

### Phase 7 — Release and re-verification

**Deliverable:** every fix proven on the host class that found it.

1. Cut **v0.13.5** carrying Phases 1–6.
2. Verify the release assets resolve (0098 Phase 0 §0.5, all six `200`).
3. Re-run the 0098 phases below, one host at a time, using the same tagging,
   dead-man switch, evidence-before-terminate gate, and teardown assertions.

| 0098 phase | Host | Proves |
|---|---|---|
| 1.3–1.4 | Alpine 3.23.5 amd64 + `apk add s6` | F1 |
| 4.1 | virgin Ubuntu 26.04, `--with-relay-service` | F4a, F4b |
| 3.1–3.2 | Rocky 9, two users contending for `127.0.0.1:7531` | F5 |
| 7.4–7.5 | Windows Server 2025, `m8i.xlarge` nested virt, WSL2/WSL1 | F6 |

**Exit criterion:** all four pass; 0097-PLAN matrix rows updated from ⚠️/❌ to
✅ with measured output; teardown assertions empty.

## Verification

Reuses 0099 §Confirmation; no competing criteria.

### Per-fix acceptance

| Fix | Command | Pass condition |
|---|---|---|
| F1 | `sh install.sh --uninstall` on an s6 host | `pgrep -f "mcremote serve"` empty; no `/proc/*/exe` → `(deleted)` |
| F4a | `systemctl --user is-active mcrelay` | `active`; journal has no `218/CAPABILITIES` |
| F4b | `mcrelay serve --config ~/.config/mcrelay/config.yaml` | starts; no `plaintext listen ... refused` |
| F5 | second user installs while `127.0.0.1:7531` is held | reports `FAILED to start`, exit **3**, journal lines shown |
| F6 | WSL2 `systemd=false`, and WSL1 | output has wsl.conf/WSL1 remedy, `pam_systemd` absent |
| — | healthy Ubuntu install | still `supervised+boot`, exit 0 — no regression |

### Cross-cutting

```sh
sh -n scripts/install.sh
shellcheck -s sh scripts/install.sh
tail -1 scripts/install.sh          # exactly: main "$@"
sh scripts/install_test.sh          # all pass, count >= 65
go test ./internal/cli/service/...
```

### The regression guard

Case 15 is the plan's most important artifact: it expresses 0097 §4.0
(*"Never claim boot persistence that was not configured"*) as an executable
assertion. **Confirm it fails against the v0.13.4 script before accepting it** —
a guard that passes on the buggy code guards nothing.

## Rollback

Each phase is a separate commit and independently revertible.

| Phase | Risk | Rollback | Trigger |
|---|---|---|---|
| 1 (gate) | false `failed` on slow hosts | revert the commit; or ship with `MCREMOTE_SVC_WAIT_SECS` raised | any host reporting `failed` for a service that is in fact healthy |
| 2 (s6) | probe wrong in the other direction | revert; `svc_is_active` returning false is the status quo, not a new break | s6 host reports active when down |
| 3 (relay unit) | losing two hardening directives | revert the template; they were never functional in user scope, so reverting restores a crash loop, not security | evidence the directives ever worked under a user manager |
| 4 (relay config) | operator expected a public bind | revert `defaults_mcrelay.yaml`; only affects **newly written** configs, never an existing one | operator reports a provisioned relay unreachable where it used to bind publicly |
| 5 (WSL) | none — removes text | revert | — |
| 6 (docs/tests) | none | revert | — |

**Irreversible boundary:** none. No published artifact is rewritten and no
already-installed host is modified — `setup-service` refuses to overwrite a
differing unit without `--force`, so existing installs keep their current unit
until the operator opts in.

**Blast-radius note:** Phase 4 changes a *provisioning* default. Hosts already
running a relay keep their config; only a fresh `setup-service` sees loopback.

## Task Checklist

**Phase 1 — verification gate**

- [ ] `SVC_WAIT_SECS` with `MCREMOTE_SVC_WAIT_SECS` override
- [ ] `svc_settle()` — active / slow / looping, using `NRestarts` to separate the last two
- [ ] `svc_diagnose()` per backend
- [ ] Gate applied at `:306` and `:331`; relay gated when `--with-relay-service`
- [ ] `summary()` gains `starting` and `failed`
- [ ] Exit codes: `starting`→0, `failed`→3
- [ ] Healthy-path output unchanged apart from latency

**Phase 2 — s6**

- [ ] `svc_is_active` uses `s6-svstat`, matching `^up `
- [ ] `s6-svstat` added to the `detect_init` s6 arm
- [ ] `shellcheck -s sh` clean

**Phase 3 — relay unit**

- [ ] `PrivateDevices` and `RestrictNamespaces` removed
- [ ] Exclusion comment rewritten, incl. "not container-specific" correction
- [ ] `grep -n 'PrivateDevices\|RestrictNamespaces' internal/cli/service/*.go` — all sites found
- [ ] `TestRenderUnitMcrelay` (:98) updated: moved to negative assertions
- [ ] `TestSetupWritesDefaultMcrelayConfig` (:180) standalone check removed
- [ ] `TestRenderUnitMcremote` still passes untouched
- [ ] `go test ./internal/cli/service/...` green

**Phase 4 — relay config**

- [ ] `defaults_mcrelay.yaml` `listen.host: "127.0.0.1"` + comment
- [ ] `internal/relay/fileconfig.go:177` deliberately unchanged, noted in commit
- [ ] `setup-service` note states the shipped default
- [ ] `template_parity_test.go` passes (update example config if needed)

**Phase 5 — WSL advisory**

- [ ] `advisories()` guard on `wsl1`/`wsl2`
- [ ] WSL2/WSL1 remedies still print

**Phase 6 — harness and docs**

- [ ] Cases 15–22 added; `MCREMOTE_SVC_WAIT_SECS=1` in the harness
- [ ] **Case 15 confirmed failing against the v0.13.4 script**
- [ ] Suite ≥ 65 assertions, all pass
- [ ] `0097-PLAN` §4.G vocabulary updated
- [ ] `ops-linux-install.md`: vocabulary, `s6-svstat`, relay loopback default

**Phase 7 — release and re-verify**

- [ ] v0.13.5 cut; six assets return `200`
- [ ] 0098 Phase 1.3–1.4 re-run → F1 closed
- [ ] 0098 Phase 4.1 re-run → F4a + F4b closed
- [ ] 0098 Phase 3.1–3.2 re-run → F5 closed
- [ ] 0098 Phase 7.4–7.5 re-run → F6 closed
- [ ] Healthy-path regression check on Ubuntu and Rocky
- [ ] 0097-PLAN rows moved ⚠️/❌ → ✅ with measured output
- [ ] Teardown assertions empty; evidence collected before every terminate
- [ ] MADR 0099 → `accepted`
