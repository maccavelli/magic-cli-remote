---
status: accepted
date: 2026-08-18
decision-makers: Project Owner (scope, severity, and release gating)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Verify service state before reporting it, and repair the s6 and relay backends

## Context and Problem Statement

The [MADR 0098](0098-MADR-ephemeral-cloud-install-verification.md) sweep ran all
twelve outstanding [0097-PLAN](0097-PLAN-linux-curl-installer.md) acceptance
rows on real hosts and returned
[seven findings](0098-findings-install-verification-sweep.md), two of them HIGH.
Every one landed in a row that table had marked untested.

Read as a list they look like four unrelated bugs. They are not. MADR 0097 §4.0
set an explicit honesty contract for this installer:

> Therefore every backend below is classified by the two capabilities it can
> actually deliver … **Never claim boot persistence that was not configured.**

That contract is **stated but unenforced**. `install.sh` decides what to print
from whether a *setup command returned zero*, never from whether the service is
actually running. Three of the four defects are the same failure of that
contract, wearing different clothes:

| Finding | What was claimed | What was true |
|---|---|---|
| **F5** | `service: running, and enabled at boot` | unit in `auto-restart`, `MainPID=0` |
| **F4** | relay service set up | unit that can never start, on any host |
| **F1** | `stopping any running service…` | nothing stopped; daemon survives on a `(deleted)` inode |

`setup_service()` sets `SERVICE_RESULT="supervised+boot"`
(`scripts/install.sh:331`) on a zero exit from `mcremote setup-service`. But
`systemctl --user enable --now` returns zero once the start job is *issued*, not
once the unit is *running*. `summary()` (`:445`) then prints a capability claim
that was never measured.

The stakes are set by v0.13.4 being public with the `curl | sh` one-liner in the
README: this is the **first** thing a new user runs, and a false success is
strictly worse than a failure, because it sends them away believing they have a
working daemon.

The question is not "should we fix these bugs" but **what change makes this
class of defect stop recurring**, given that the sweep found the same root cause
three times from three unrelated triggers.

## Decision Drivers

* **D1 — A false success is the worst outcome.** Silent staleness (F1) and
  silent non-start (F4/F5) both leave the user confidently wrong. The code
  comments already argue this: *"Silent staleness is worse than a crash."*
* **D2 — Enforce the 0097 contract mechanically, not by discipline.** The
  contract has existed since 0097 and was violated anyway. A rule that depends
  on remembering it will be violated again.
* **D3 — No new privilege.** Every remedy must hold inside the no-root,
  `$HOME`-scoped design. A fix that needs `sudo` is not a fix.
* **D4 — Do not regress what works.** systemd, runit, `openrc-system`, arm64,
  musl, checksum verification, and version pinning were all measured correct.
  The blast radius must stay inside the defective paths.
* **D5 — Re-verifiable by the same sweep.** Each fix must be confirmable by
  re-running a specific 0098 phase, so the fix is proven the way the defect was.
* **D6 — Honest degradation over silent success.** Where a backend genuinely
  cannot deliver a capability, saying so plainly is an acceptable outcome;
  claiming it is not.

## Considered Options

1. **A verification gate plus targeted backend repairs** — make
   `SERVICE_RESULT` a *measured* value rather than an assumed one, then fix the
   s6 probe and the relay unit template behind it.
2. **Point fixes only** — correct `s6-svc -l`, the relay unit directives, the
   relay default config, and the WSL advisory. Leave the reporting logic alone.
3. **Remove the defective backends** — delete `--with-relay-service` and the s6
   backend rather than repairing them.
4. **Defer** — document the findings, ship as is, wait for user reports.

## Decision Outcome

Chosen option: **"A verification gate plus targeted backend repairs"**, because
it is the only option that addresses **D2**. The sweep produced the same root
cause from three unrelated triggers — a bad option flag, an inapplicable systemd
directive, and a port collision — which is strong evidence that the next trigger
is simply one nobody has hit yet. Options 2 and 3 fix the three known triggers
and leave the mechanism that hid them fully intact.

The change has two parts, in this order:

**Part 1 — `SERVICE_RESULT` becomes a measurement.** After any backend claims
success, poll the backend's own liveness probe for a bounded interval and
downgrade the reported result to what was actually observed:

| Observed | Reported |
|---|---|
| active and stable | `supervised+boot` / `supervised` as configured |
| still activating after the window | `starting` — new, honest, non-fatal |
| failed or restart-looping | `failed` — with the backend's own diagnostic |

This alone converts F4 and F5 from a green "running" into an accurate report,
*without knowing anything about why* the service failed — which is precisely the
property that makes it hold for the trigger nobody has hit yet.

**Part 2 — repair the three broken mechanisms**, each of which the gate would
otherwise merely report honestly rather than resolve:

* **F1** — replace `s6-svc -l` (`:221`) with `s6-svstat`, verified working on
  the same host where `s6-svc -l` exits 100.
* **F4a** — drop `PrivateDevices` and `RestrictNamespaces` from the `mcrelay`
  unit template; both imply capability-bounding-set changes a user manager
  cannot perform, which is the exact reason the template's own comment already
  excludes `ProtectClock`/`ProtectKernelModules`/`ProtectKernelLogs`. They
  belong on that list.
* **F4b** — `mcrelay setup-service` must not write a config the binary refuses.
  Default to a loopback bind, or emit the config with TLS configured, rather
  than `0.0.0.0:8443` with `tls=off`.
* **F6** — suppress the `su`/`pam_systemd` paragraph when `ENVIRONMENT` is
  `wsl1`/`wsl2` (`:413` guarded against `:421`).

**Explicitly not changed:** the `openrc-user` backend stays (F2 — OpenRC 0.63
supports user mode; the probe correctly declines when `XDG_RUNTIME_DIR` is
unset, and falling through to `openrc-system` is right). 0097 open question 2 is
answered "keep it".

### Consequences

* Good, because the gate is **trigger-agnostic**: it reports the truth for
  failures nobody has anticipated, which is the only property that generalises
  from three known triggers to the unknown fourth.
* Good, because it enforces the 0097 §4.0 contract in code rather than in prose,
  satisfying **D2**.
* Good, because every remedy is a small, local edit to an already-tested script,
  and none of them touch systemd, runit, `openrc-system`, checksum verification,
  or version pinning — the paths the sweep measured as correct (**D4**).
* Good, because each fix maps to a specific 0098 phase that can re-prove it
  (**D5**), and the harness for doing so already exists and cost about 40 cents.
* Bad, because a polling gate adds latency to every install — a few seconds on
  the happy path, and it must be bounded so a slow-but-fine host is not reported
  as failed. Choosing that window is a judgement call with no obviously correct
  value, and setting it too tight converts a working install into a false alarm,
  which is a new failure mode in the opposite direction.
* Bad, because `starting` and `failed` are new vocabulary in the summary, so
  0097-PLAN §4.G, `ops-linux-install.md`, and `scripts/install_test.sh`
  assertions all need updating in step; the exit-code contract (`3` = service
  setup failed) must be reconciled with a service that is merely slow.
* Bad, because Part 2's relay work is not purely mechanical: F4b is a
  *product* decision about what a default relay config should be, not a script
  fix, and it may belong to whoever owns `mcrelay`'s TLS story rather than to
  the installer.
* Neutral, because none of this changes the installer's privilege model, its
  download-and-verify path, or its uninstall semantics beyond making the stop
  step actually work.

### Confirmation

The sweep that found these defects is the fitness function that closes them.
Each fix is confirmed by re-running a specific 0098 phase against the released
artifact, not by inspection:

| Fix | Re-run | Passes when |
|---|---|---|
| F1 | Phase 1.3–1.4 (Alpine + s6) | after `--uninstall`, `pgrep -f "mcremote serve"` is empty and no `/proc/*/exe` shows `(deleted)` |
| F4a | Phase 4.1 (virgin Ubuntu) | `systemctl --user is-active mcrelay` → `active`, no `218/CAPABILITIES` in the journal |
| F4b | Phase 4.1 | relay starts on the config `setup-service` wrote, unmodified |
| F5 | Phase 3.2 (two users, one port) | second install reports `failed`, **not** `running`, and exits non-zero |
| F6 | Phase 7.4–7.5 (WSL2 no-systemd, WSL1) | output contains the wsl.conf remedy and **not** the `su`/`pam_systemd` paragraph |

Standing regression guard: `scripts/install_test.sh` gains a case asserting that
a backend whose liveness probe reports "not running" can never produce a
`supervised+boot` summary line. That is the contract from §4.0 expressed as an
assertion, and it fails today.

## Pros and Cons of the Options

### 1. Verification gate plus targeted repairs (chosen)

* Good, because it fixes the known defects **and** the mechanism that concealed
  them (**D1**, **D2**).
* Good, because the gate degrades honestly on backends where liveness cannot be
  probed at all, which is the 0097 §4.0 behaviour (**D6**).
* Good, because it is re-verifiable end to end for cents (**D5**).
* Bad, because it is the largest of the options and touches the summary
  vocabulary, with documentation and test fallout.
* Bad, because the polling window is a tunable with no self-evident value.

### 2. Point fixes only

* Good, because it is the smallest, lowest-risk change and closes both HIGH
  findings today.
* Good, because it needs no new vocabulary, so docs and tests barely move.
* Bad, because it fails **D2** outright. The reporting logic that turned three
  independent failures into a green "running" survives untouched, so the fourth
  trigger produces the same silent-success outcome.
* Bad, because F5 was observed from a cause (*a second user holding the port*)
  that no amount of backend fixing prevents — it is not a backend bug at all,
  and this option has no answer to it.
* Neutral, because it is a strict subset of the chosen option and remains the
  correct fallback if Part 1 proves too invasive to land quickly.

### 3. Remove the defective backends

* Good, because a feature that has never worked is arguably worse than no
  feature, and deleting `--with-relay-service` would be honest.
* Good, because it eliminates F4 completely with no design work on TLS defaults.
* Bad, because the s6 backend is **not** broken — the sweep measured detection,
  run-script creation, and supervision all working. Only a four-character probe
  is wrong. Deleting it would discard a working backend over a typo.
* Bad, because it does nothing for F5, which is the finding that generalises.
* Neutral, because it stays a reasonable outcome *for the relay flag
  specifically* if F4b's config question proves contentious — the chosen option
  can fall back to this for that one flag without abandoning the rest.

### 4. Defer

* Good, because it costs nothing now.
* Bad, because F1 silently leaves a daemon running from a deleted binary on
  uninstall, and F4 means a documented flag has never once worked. Both are in a
  published release fronted by a one-liner in the README.
* Bad, because the defects are invisible by construction — F5 guarantees the
  user is told everything is fine — so "wait for reports" is waiting for a
  signal the software actively suppresses.

## More Information

### Outcome

Implemented across v0.13.5 and v0.13.6 and re-verified on real hosts
2026-08-18: [0099-findings-reverification.md](0099-findings-reverification.md).
All five fixes confirmed against published artifacts.

The sweep vindicated the decision to prefer the trigger-agnostic gate over
point fixes, in the most direct way available: **v0.13.5's gate still reported
success for a crash-looping daemon**, because `Type=simple` marks a unit active
the instant the process is forked. Only real-host testing surfaced it. The gate
now requires sustained active plus two loop signals (v0.13.6).


### Finding-to-remedy map

| # | Sev | Finding | Remedy | Where |
|---|---|---|---|---|
| F1 | HIGH | `s6-svc -l` is not a valid option; probe always false | use `s6-svstat` | `install.sh:221` |
| F4a | HIGH | `PrivateDevices`+`RestrictNamespaces` ⇒ `218/CAPABILITIES` in user scope | remove both from the relay template | `mcrelay setup-service` |
| F4b | HIGH | default config `0.0.0.0:8443` + `tls=off` is refused by the binary | write a startable default | `mcrelay setup-service` |
| F5 | MED | `supervised+boot` reported without measuring | verification gate | `install.sh:331`, `:445` |
| F6 | MED | `su`/`pam_systemd` advisory shown on WSL, cause factually false there | suppress when `ENVIRONMENT` is `wsl*` | `install.sh:413` |
| F2 | — | `openrc-user` unreachable on stock Alpine | **no change** — backend is correct | — |
| F3 | — | deny-all NACL on `subnet-7e5b3912` | environment, not product | 0098-PLAN |
| F7 | — | Windows/WSL provisioning prerequisites | documented | 0098 findings |

### Why F5 is the load-bearing fix

F1 and F4 are both *reachable* today only because F5 hides them. Fixing F1 and
F4 without F5 leaves the installer able to report success for any future service
that fails to start — and the sweep already demonstrated one such cause
(port collision) that is not a backend defect and cannot be fixed in a backend.
That is the argument for ordering Part 1 before Part 2, and for not accepting
option 2 as the whole answer.

### Deliberately out of scope

* **The `sysvinit` classification.** `detect_init` emits no `sysvinit` value and
  a converted Debian host is classified `systemd-broken`. Row 8b was blocked
  before this could be measured (0098 §Row 8b BLOCKED), so there is no evidence
  to decide on. It needs measurement before it needs a decision.
* **`--with-relay-service` on non-systemd backends**, where the flag is silently
  ignored (`svc_runit`/`svc_s6`/`svc_openrc` write a unit for `mcremote` only).
  Real, but distinct from F4, and untested by the sweep.
* **`mcrelay`'s TLS defaults** beyond making `setup-service` emit something that
  starts. The broader question of what a relay should bind by default belongs to
  MADR 0091's owner.

### Related records

* [0098-MADR](0098-MADR-ephemeral-cloud-install-verification.md) /
  [0098-PLAN](0098-PLAN-ephemeral-cloud-install-verification.md) — the sweep
  that produced these findings, and the harness that re-verifies the fixes.
* [0098-findings-install-verification-sweep.md](0098-findings-install-verification-sweep.md)
  — measured evidence, verbatim output, per finding.
* [0097-MADR](0097-MADR-linux-curl-installer.md) /
  [0097-PLAN](0097-PLAN-linux-curl-installer.md) — the installer, its §4.0
  honesty contract, and the acceptance matrix now fully executed.
* [0091-MADR-mcrelay-daemon-hardening.md](0091-MADR-mcrelay-daemon-hardening.md)
  — origin of the relay unit's hardening directives, including the D4 probe that
  covered `RestrictAddressFamilies` and `MemoryDenyWriteExecute` but not the two
  directives that turned out to break it.
* [ops-linux-install.md](ops-linux-install.md) — backend capability table and
  advisories; updated when the summary vocabulary changes.
