---
status: accepted
date: 2026-11-08
decision-makers: Project Owner
consulted: "this repository at 4831e6c5, read from code and git history; 0099-MADR/PLAN (F4a) and its fix 14ea9749; 0091-MADR (D4, D7); 0054-PLAN and 0207-PLAN (the deploy examples left unchanged); 0239's P4 finding; a directive-level comparison of each deploy user unit with its embedded template (unit_diff.py, session scratchpad)"
informed: anyone who installs mcrelay or mcremote from the deploy/ examples by hand
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Make the shipped user units match the templates, and gate them so they cannot drift again

## Context and Problem Statement

The repository ships two ways to install mcremote and mcrelay as a systemd user service:

* `mcremote setup-service` / `mcrelay setup-service`, which render the embedded templates
  `internal/cli/service/mcremote.user.service.tmpl` and `mcrelay.user.service.tmpl`;
* the static examples `deploy/systemd/mcremote.user.service` and `deploy/systemd/mcrelay.user.service`, which the
  guides offer as the manual path.

The two have drifted. The worst case: **the shipped manual mcrelay unit can never start.** The 0239 audit found it
(its P4 finding), and the owner asked for it to be resolved with a full record.

### F1. The manual mcrelay unit sets the two directives that make a user unit unstartable [observed]

`deploy/systemd/mcrelay.user.service:131-132` sets:

```ini
# mcrelay does not exec coding CLIs, so these are safe here.
PrivateDevices=true
RestrictNamespaces=true
```

[0099-MADR-installer-service-state-verification.md](0099-MADR-installer-service-state-verification.md) F4a found
that these two make a systemd **user** unit unstartable on every host, not only container-restricted ones. Both
imply capability-bounding-set changes a user manager cannot make. The 0099 sweep reproduced it on a stock Ubuntu
26.04 EC2 instance:

```text
mcrelay.service: Failed to drop capabilities: Operation not permitted
Failed at step CAPABILITIES ... status=218/CAPABILITIES
```

With `Restart=always` that is a permanent crash loop. Removing both was necessary and sufficient; neither alone
cleared it (0099-PLAN, "F4a — relay unit hardening directives").

*Amended 2026-11-08 (A1, below): "every host" overstates 0099's evidence, which is one stock Ubuntu 26.04 EC2
instance. On the Linux test host, with Ubuntu's user-namespace restriction turned off, the unit as it was started
and ran (F10).*

The embedded template is fixed and says so (`mcrelay.user.service.tmpl:73-87`). The deploy file still carries the
directives, and its comment (`:137-140`) blames "container-restricted hosts". 0099 records that reasoning as the
reason the pair was wrongly thought safe, and says not to reintroduce them on it.

### F2. The guides send readers to that file [observed]

* `docs/guides/ops-mcrelay.md:38` lists it as "Manual unit; comments list every `MCRELAY_*` env", and `:83` gives it
  as the manual install.
* `README.md`'s mcrelay table lists it as "User unit (all env commented)", and the Deploy table as the "mcrelay user
  unit".
* The file's own header (`:7-13`) gives the manual install: copy it to `~/.config/systemd/user/mcrelay.service`,
  `daemon-reload`, `enable --now`.

Anyone who has followed that path since 2026-08-15 has a unit in a 218/CAPABILITIES restart loop.

### F3. How it happened [observed, git history]

| Date | Commit | What changed |
| :--- | :--- | :--- |
| 2026-07-26 | `af369c6c` | An unrelated feature commit ("feat(session): add session renaming …") adds `Environment=GOCACHE=/cache/go` and `GOTMPDIR=/cache/tmp` to **both** deploy user units, noted as "persistent GOCACHE/GOTMPDIR locations". No record decided it, and neither template has ever set them. |
| 2026-08-15 | `e23db2f1` | `PrivateDevices` and `RestrictNamespaces` are added to the mcrelay template **and** the deploy file. |
| 2026-08-18 | `14ea9749` | 0099 F4a's fix drops both from the **template** only. 0099-PLAN's inventory and its check (`grep -n 'PrivateDevices\|RestrictNamespaces' internal/cli/service/*.go`, PLAN step list) covered `internal/cli/service`, never `deploy/`. |
| 2026-10-26 | `2d6efd62` | The mcrelay template moves to `Type=notify` with `TimeoutStartSec=240` (0207 D22). The deploy file keeps `Type=simple`, by decision (F5). |

### F4. Nothing checks the deploy units' behaviour [observed]

* `make verify-units` (`Makefile:481-499`), run by CI's "Validate systemd units" step (`ci.yml:164-165`), runs
  `systemd-analyze verify` on each `deploy/systemd/*.service`. That checks syntax and references. It cannot see a
  user manager failing to drop capabilities at start, so the unstartable unit passes CI.
* No test compares a deploy unit with its template. 0054-PLAN says so in as many words: "No code compares them with
  the template."
* The template's own tests ban the two directives (`setup_test.go:65-66` for mcremote, `:186` for mcrelay). The
  deploy files have no such test.
* There is no live systemd user-manager test in CI. The live-supervisors job covers runit and s6 only.

### F5. Two directive differences were decided on purpose [observed, records]

* 0054-PLAN (mcremote, S8) and 0207-PLAN (mcrelay, D22) both left the deploy examples at `Type=simple` while the
  templates moved to `Type=notify`. 0207: "`Type=simple` works with every release, and `make verify-units` checks
  them as they are."
* So `Type` and the `TimeoutStartSec` that goes with `notify` differ by decision, not by drift.

### F6. The full drift, per directive [observed, unit_diff.py]

| Unit | Only in the deploy file | Only in the template | Different value |
| :--- | :--- | :--- | :--- |
| mcrelay | `PrivateDevices=true`, `RestrictNamespaces=true`, `GOCACHE=/cache/go`, `GOTMPDIR=/cache/tmp` | `USER`, `LOGNAME`, `XDG_STATE_HOME`, `XDG_RUNTIME_DIR` (`%t`), `TimeoutStartSec=240` | `Type`: `simple` against `notify` |
| mcremote | `GOCACHE=/cache/go`, `GOTMPDIR=/cache/tmp` | `USER`, `LOGNAME`, `XDG_STATE_HOME`, `XDG_RUNTIME_DIR` (`%t`), `TimeoutStartSec=240` | `Type`: `simple` against `notify` |

Every hardening directive other than the mcrelay pair, and `Restart`, `RestartSec`, `TimeoutStopSec`, `KillMode`,
`KillSignal`, `UMask`, `LimitNOFILE` and the `[Unit]`/`[Install]` keys, matches.

### F7. The `/cache` variables break agents' Go commands on most hosts [inferred from code and the variables' meaning]

mcremote runs coding agents (grok, opencode, codex, kilo) as children, and they inherit its environment. An agent
that runs `go build` or `go test` uses `GOCACHE` and `GOTMPDIR`. `/cache` is not a standard path: on a host without
a writable `/cache`, every Go command an agent runs fails creating the cache or temp directory. The templates leave
both unset, so Go uses `$XDG_CACHE_HOME/go-build` and the system temp directory. mcrelay runs no Go toolchain, so
there the variables are dead weight. Not reproduced live for this record; the PLAN's acceptance does.

### F8. The "every variable" claim [observed]

The relay's non-test sources in `internal/relay` name 36 `MCRELAY_*` variables. Three more, `MCRELAY_D4_URL`,
`_HOST` and `_SECRET`, appear only in the 0091 D4 live probe test, `d4_live_test.go`. The deploy unit's comments
list 35. The missing one is
`MCRELAY_LIMITS_MAX_CONNS`. 0239 P4 changed `config-mcrelay.md` to say "most" for this reason.

### F9. Out of scope, checked [observed]

* **The system units** `deploy/systemd/mcrelay.service` and `mcremote.service`. The mcrelay one sets both directives,
  correctly: a system manager can apply them, as 0091 D7 records. They are not user units.
* **The launchd examples** `deploy/launchd/*.plist`. They carry the same key set as `plist_render.go` and no `/cache`
  path. There is no counterpart to F1 or F7.

## Decision Drivers

* A unit the repository ships and documents must start on a stock host.
* One source of truth for each directive: the template, which `setup-service` installs and its tests guard.
* A drift like F1 must fail CI, not wait for an audit. `systemd-analyze verify` cannot catch it (F4).
* Decisions already taken stand unless reopened: `Type=simple` for the static examples (F5).
* Smallest change that is complete: no change to the templates, the renderer, or `setup-service`.

## Considered Options

* **A.** Remove the two directives from the mcrelay deploy unit, and nothing else
* **B.** Align both deploy user units with their templates, apart from the two decided differences, and add a test
  that compares them
* **C.** Delete the deploy user units, and point the manual path at `setup-service --print-only`
* **D.** Generate the deploy user units from the templates, and check the generated files in

## Decision Outcome

Chosen option: **B**, because it makes the shipped manual units correct today, fixes F7 along with F1, keeps the
decided `Type=simple`, and adds the gate F4 shows is missing. A fixes only the symptom the audit happened to see.

* **D1. The mcrelay deploy unit drops `PrivateDevices=true` and `RestrictNamespaces=true`** (F1). Their comment, and
  the stale "container-restricted hosts" exclusion comment (`:137-140`), are replaced with the template's 0099 F4a
  block, so the file explains the absence the way the template does.
* **D2. Both deploy user units drop `GOCACHE=/cache/go` and `GOTMPDIR=/cache/tmp`** (F7). No record chose them, and
  the templates do not set them.
* **D3. Both deploy user units gain the template's environment:** `USER=%u`, `LOGNAME=%u`,
  `XDG_STATE_HOME=%h/.local/state` and `XDG_RUNTIME_DIR=%t`, the specifier forms of the template's rendered values.
* **D4. `Type=simple` stays, with no `TimeoutStartSec`** (F5, 0054, 0207; the owner's answer to Q1, "keep simple").
  Each file gains a one-line comment naming the difference and why: it works with every release, and
  `setup-service` installs `Type=notify`.
* **D5. A parity test, `TestDeployUserUnitsMatchTheTemplates`, in `internal/cli/service`.** For each product it
  renders the template through `RenderUnit` with fixed options, parses the directives of the rendered unit and of
  the deploy file, and fails unless:
  * the two have the same directive keys in `[Unit]`, `[Service]` and `[Install]`, apart from an explicit allow
    list (`Type`, `TimeoutStartSec`, and the commented `Environment=` lines, which are not directives);
  * every directive with a literal template value (the hardening set, `Restart`, `RestartSec`, `TimeoutStopSec`,
    `KillMode`, `KillSignal`, `UMask`, `LimitNOFILE`, `StartLimit*`) has the same value;
  * neither deploy user unit sets `PrivateDevices` or `RestrictNamespaces`, named as 0099 F4a in the failure;
  * neither sets a path under `/cache/`.

  Each failure names the directive, the file, and the template line. The allow list lives in the test, with its
  reason beside each entry, so widening it is a reviewed change.
* **D6. The mcrelay deploy unit lists every relay variable** (F8). It gains `MCRELAY_LIMITS_MAX_CONNS`. A second
  test, `TestDeployRelayUnitNamesEveryRelayVariable`, reads the `MCRELAY_*` literals in `internal/relay`'s non-test
  sources (which do not include the D4 probe's three) and fails on any the unit does not name. `config-mcrelay.md` goes back to
  "every".
* **D7. The docs say what changed and what to do on an affected host:**
  * `ops-mcrelay.md` gains a note: a unit copied from `deploy/systemd/mcrelay.user.service` before this change
    crash-loops with `218/CAPABILITIES`. Remove the two directives from it, or replace it with
    `mcrelay setup-service --force`, then `daemon-reload` and restart. The same note covers the `/cache` lines in
    either manual unit.
  * `README.md`'s "mirrors embedded unit" becomes true, and names the `Type=simple` difference.
* **D8. The fix is shown working on a real user manager.** On the Linux test host, under a scratch unit name, the
  unit as it is today is started and seen failing with `218/CAPABILITIES`. The fixed unit is then started and seen
  `active`, with no `218` in its journal. The owner's own services are not touched. This is the acceptance F4 says
  CI cannot give.
  *Amended 2026-11-08 (A1, D11): the run happens with the host's user-namespace restriction turned on.*
* **D9. Not changed:** the templates, `RenderUnit`, `setup-service`, the system units, and the launchd examples
  (F9). `make verify-units` keeps checking syntax; the parity test is the behavioural gate.
  *Amended 2026-11-08 (A1, D12): the mcrelay template's F4a comment changes. No directive changes.*
* **D10. 0099 gets a dated note** pointing here: F4a's fix covered the template, and this record covers the deploy
  copy it missed.

### Consequences

* Good, because the documented manual mcrelay install starts again, on every host 0099 tested.
* Good, because agents under a manually installed mcremote unit get a working Go cache again.
* Good, because the next drift between a deploy user unit and its template fails CI, with a message that names it.
* Good, because the two decided differences stay, and are now written down in the files and the test.
* Neutral, because a host that already copied the old unit keeps it until its owner edits or replaces it. Nothing
  in the daemon can rewrite a file it did not install; D7's note says what to do.
* Bad, because the static examples remain a second copy of each unit. The test bounds the drift; it does not remove
  the copy (option C would).
* Bad, because the parity test parses unit text with its own small parser. A directive spelled differently, or
  split across lines, could slip past it. The allow list and the explicit bans cover the cases this record found.

### Confirmation

1. `grep -n 'PrivateDevices\|RestrictNamespaces' deploy/systemd/*.user.service` finds nothing, and
   `grep -rn '/cache/' deploy/systemd/` finds nothing.
   *Amended 2026-11-08 (owner's choice at PLAN P5): the first grep, as written, matches the F4a comment that D1
   requires. The check is for directive lines:
   `grep -nE '^[[:space:]]*(PrivateDevices|RestrictNamespaces)[[:space:]]*=' deploy/systemd/*.user.service`.*
2. `TestDeployUserUnitsMatchTheTemplates` and `TestDeployRelayUnitNamesEveryRelayVariable` pass. Each was seen
   failing first: on the deploy files as they are today, and again on a scratch copy with one planted break per
   rule.
3. `make verify-units`, `make test`, `make race` and `make pre-add-check` on the new test file pass; CI passes on the
   push.
4. On the Linux test host: the current mcrelay deploy unit, under a scratch name, fails with `218/CAPABILITIES`; the
   fixed one reaches `active` with no `218` in its journal; the scratch unit and its files are removed afterwards.
   *Amended 2026-11-08 (A1, D11): both runs happen with `kernel.apparmor_restrict_unprivileged_userns=1`, and
   afterwards the sysctl reads the value it had before the run.*
5. The docs carry D7's note, `config-mcrelay.md` says "every", and `make check-records` and markdown lint pass.
6. *Added 2026-11-08 (A1, D12):* none of D12's places says "every host" or "never starts". The test's F4a message
   is seen once more on the planted `PrivateDevices=true` break, now with the narrowed wording.

## Pros and Cons of the Options

### A. Remove the two directives, and nothing else

* Good, because it is a two-line change that fixes F1.
* Bad, because F7's `/cache` variables stay, breaking agents' Go builds under a manual mcremote unit.
* Bad, because nothing stops the next drift, which is how F1 happened (F3, F4).

### B. Align both deploy user units, and add a parity test (chosen)

* Good, because it fixes every drift F6 lists, apart from the two decided ones.
* Good, because the test fails CI on the class of error, not only this instance.
* Bad, because the deploy files remain a second copy to keep in step, now enforced rather than hoped for.

### C. Delete the deploy user units; use `setup-service --print-only`

* Good, because one copy is better than two: `--print-only` prints exactly what `setup-service` would install,
  with this host's paths.
* Bad, because the deploy mcrelay unit's commented list of every `MCRELAY_*` variable is the reference the guides
  point to (F2). It would have to move.
* Bad, because a user who wants a file to read before running anything loses it, and links in the README, the
  guides and 0015's E4 deliverable break.

### D. Generate the deploy user units from the templates

* Good, because the files could not drift: CI would regenerate and diff them.
* Bad, because `RenderUnit` renders absolute paths for the current user, not `%h` specifiers. A generator would need
  options that `normalize` may refuse, or a second rendering mode. That is a change to the renderer, which D9 keeps
  out.
* Bad, because the generated `Type=notify` would reverse F5's decided `Type=simple`.

## More Information

### Amendment A1 — 2026-11-08: the 218 needs a host that restricts user namespaces

PLAN P4 started the mcrelay deploy unit as it was before the fix (`6178ad32^`) on the Linux test host. The unit did
not fail. PLAN P4 step 4 stopped the phase, and the owner chose how to continue (below).

#### F10. The old unit runs on a host without Ubuntu's user-namespace restriction [observed]

On the Linux test host (Ubuntu 26.04.1, systemd 259.9-0ubuntu0.1, kernel 7.0, a KVM guest), under the scratch unit
name `mcrelay-0240-check`, the old unit reached `ActiveState=active` and `SubState=running` with `NRestarts=0`. The
journal had no line matching `218` or `CAPABILITIES`, and mcrelay logged `listening`. Read-only probes of the same
host:

* `kernel.apparmor_restrict_unprivileged_userns = 0`, set by `/etc/sysctl.d/60-mcremote-userns.conf`. That file is
  written by this repository's `scripts/bwrap-apparmor-fix.sh`
  ([0048-MADR-codex-sandbox-namespace.md](0048-MADR-codex-sandbox-namespace.md)), so the codex sandbox can create
  user namespaces. 0048 records the stock Ubuntu value as `1`.
* `unshare -U true` as the user: exit 0.
* The user manager's capability sets: `CapEff` = `0000000800000000`, which is `CAP_WAKE_ALARM` alone, with no
  `CAP_SETPCAP`.

0099's reproduction was a stock Ubuntu 26.04 EC2 instance, which has the restriction on.

**Hypothesis, tested by D11:** a user manager without `CAP_SETPCAP` cannot change a unit's capability bounding set
itself, and `PrivateDevices=` and `RestrictNamespaces=` both need that change. So it can apply them only inside a
user namespace it creates. With AppArmor's restriction on, that namespace gets no capabilities, the change fails
with `EPERM`, and systemd reports `218/CAPABILITIES`. Without the restriction, the change succeeds. This explains
both 0099's result and F10. It is not yet shown.

#### Decisions

* **D11. P4 reproduces the 218 by turning the restriction on, on the same host** (owner's choice, 2026-11-08: "Flip
  sysctl on test host"). The P4 script:
  * records the sysctl's current value;
  * sets it to `1` with `sudo -n sysctl -w`, a runtime change that leaves the drop-in file alone, so a reboot also
    restores it;
  * runs the old unit, then the fixed one;
  * restores the recorded value in a `finally` block, and checks that it reads back.

  The cost is about a minute with the codex sandbox restricted on that host. The rejected alternative was a fresh
  stock Ubuntu EC2 instance as 0099 used. It matches 0099's environment exactly and leaves the test host alone, but
  it costs AWS spend, setup and teardown, and does not test the hypothesis by changing one variable. If the old unit
  still does not fail with the restriction on, P4 stops again and nothing in D12 changes.
* **D12. Once D11 confirms it, the wording says which hosts fail.** "Every host" and "never starts" become: hosts
  where the user manager cannot create a user namespace with capabilities, which includes stock Ubuntu 24.04 and
  later. The places that change:
  * this record's F1, by the dated note already there;
  * `internal/cli/service/deployunits_test.go`: the `bannedUserUnitDirectives` comment and the F4a failure message;
  * `internal/cli/service/mcrelay.user.service.tmpl` and its copy in `deploy/systemd/mcrelay.user.service`: the F4a
    comment, which says "EVERY host". This is a comment-only change to a template, which D9 otherwise excludes;
  * `docs/guides/ops-mcrelay.md`: the note from D7, which says "never starts";
  * the dated note this record added to
    [0099-MADR-installer-service-state-verification.md](0099-MADR-installer-service-state-verification.md) F4a.

  The ban stays. The directives break a stock Ubuntu host, and a unit file cannot know which kind of host it will
  land on.

### Owner questions, answered 2026-11-08

* **Q1. `Type` for the static examples.** Keep `Type=simple`, as 0054 and 0207 decided, or move both to
  `Type=notify` with `TimeoutStartSec=240` to match the templates? `notify` needs a release from after 0207 D22 for
  mcrelay, and after 0054 S8 for mcremote. **Answer: "keep simple".** D4 stands.
* **Q2. The live acceptance.** D8 runs a scratch unit on the Linux test host: start, observe, stop, remove. It
  touches no existing service. Approve it as written? **Answer: "approved to test".** PLAN P4 runs as written.

### Related

* [0099-MADR-installer-service-state-verification.md](0099-MADR-installer-service-state-verification.md) F4a, and its
  PLAN.
* [0091-MADR-mcrelay-daemon-hardening.md](0091-MADR-mcrelay-daemon-hardening.md) D4 and D7.
* [0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md](0054-PLAN-standardize-self-update-on-go-selfupdate-lib.md)
  and [0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md](0207-PLAN-adopt-go-selfupdate-lib-v1-19-0.md): the deploy
  examples left at `Type=simple`.
* [0239-REPORT-record-status-and-docs-accuracy-audit.md](../reports/0239-REPORT-record-status-and-docs-accuracy-audit.md):
  the P4 finding.
* Why a new number rather than an amendment of 0099: 0099 is accepted and complete, and this record also covers the
  `/cache` variables, the mcremote unit and a new gate, which are not 0099's subject.
* The PLAN: [0240-PLAN-make-the-shipped-user-units-match-the-templates-and-gate-them.md](0240-PLAN-make-the-shipped-user-units-match-the-templates-and-gate-them.md).
