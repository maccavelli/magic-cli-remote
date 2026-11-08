---
status: in-progress
date: 2026-11-08
associated-madr: "0240-MADR-make-the-shipped-user-units-match-the-templates-and-gate-them.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement making the shipped user units match the templates, and gating them

Associated MADR: [0240-MADR-make-the-shipped-user-units-match-the-templates-and-gate-them.md](0240-MADR-make-the-shipped-user-units-match-the-templates-and-gate-them.md)

## Goal

When this plan is done:

* `deploy/systemd/mcrelay.user.service` starts on a stock systemd user manager: no `PrivateDevices`, no
  `RestrictNamespaces` (MADR D1);
* neither deploy user unit sets `GOCACHE` or `GOTMPDIR` to `/cache/…` (D2), and both carry the template's `USER`,
  `LOGNAME`, `XDG_STATE_HOME` and `XDG_RUNTIME_DIR` (D3);
* both keep `Type=simple`, each with a comment saying why (D4; the owner's answer to Q1);
* two tests fail CI on any future drift: `TestDeployUserUnitsMatchTheTemplates` (D5) and
  `TestDeployRelayUnitNamesEveryRelayVariable` (D6);
* the guides tell an affected host what to do (D7), and the fix is shown working on a real user manager (D8).

## Scope

**In scope:**

* `deploy/systemd/mcrelay.user.service` and `deploy/systemd/mcremote.user.service`;
* `internal/cli/service/deployunits_test.go` (new): the two tests and their unit-text parser;
* `docs/guides/ops-mcrelay.md`, `docs/guides/config-mcrelay.md`, `README.md`;
* the records: this pair, a dated note on `0099-MADR-installer-service-state-verification.md`, a resolution note in
  `docs/reports/0239-REPORT-record-status-and-docs-accuracy-audit.md`'s P4 finding, and `docs/README.md`'s index.

**Out of scope (MADR D9):** the templates (`internal/cli/service/*.tmpl`), `RenderUnit` and `setup-service`; the
system units `deploy/systemd/mcrelay.service` and `mcremote.service`; `deploy/launchd/*`; `make verify-units`.

## Conventions every phase follows

* **C1. Start state.** Each phase starts on a clean `master` equal to `origin/master`, or names what differs.
* **C2. Seen failing.** Every new test is seen failing before the fix it guards: first on the deploy files as they
  are, then on a scratch copy of the fixed files with one planted break per rule. The record quotes each failure.
  Scratch copies live in the session scratchpad, never in the tree.
* **C3. Go gates:** `make pre-add-check FILES="internal/cli/service/deployunits_test.go"`,
  `go test -count=1 ./internal/cli/service/`, `make race`, and `make verify-units` where `systemd-analyze` exists.
  `make staticcheck` and `make vet-tags` run before staging.
* **C4. Identifiers.** No hostname, account name or real-machine path in any committed file, test fixtures included.
  The test renders with `/home/user/…`, as `TestRenderUnitMcrelay` does. The redact preview runs before each staging.
* **C5. Commits.** The agent stages; the owner commits with `git commit --no-edit`. Push is the owner's.
* **C6. Hosts.** The live acceptance (P4) runs only on the owner's word, under a scratch unit name, and leaves no
  file behind. Commands that run on a host are Python scripts run from the scratchpad.
* **C7. Deviations** stop the phase and get a dated entry here, and in the MADR where a decision or fact changes.

## Implementation Steps

### P0 — The records

1. This pair, with the owner's answers to Q1 and Q2 recorded in the MADR.
2. `0099-MADR-installer-service-state-verification.md`: a dated italic note under F4a. Its fix covered the template
   (`14ea9749`); the deploy copy it missed is 0240's D1. 0099's status stays `accepted`.
3. `python3 scripts/check_records.py --write-index`; `make check-records`; the redact preview. **Stage.**

### P1 — The tests, seen failing on today's files (MADR D5, D6)

Files: `internal/cli/service/deployunits_test.go` (new).

1. **The parser.** A test helper `unitDirectives(text string) map[string][]string`, keyed `"[Section] Key"`. It skips
   blank lines and `#` comments, takes `Key=Value` lines, and keys `Environment=` lines by variable name
   (`"[Service] Environment:HOME"`). It is unexported, in the `service_test` package, with a short doc comment.
2. **The repository root.** The test finds `deploy/systemd/` by walking up from the package directory to the
   directory holding `go.mod`, so it does not depend on the working directory beyond the package.
3. **`TestDeployUserUnitsMatchTheTemplates`**, one subtest per product (`mcrelay`, `mcremote`):
   * render with `service.RenderUnit(service.Options{Product: p, UnitName: p, Binary: "/home/user/.local/bin/" + p,
     ConfigPath: "/home/user/.config/" + p + "/config.yaml", PrintOnly: true})`;
   * read `deploy/systemd/<p>.user.service`;
   * **keys:** the two directive-key sets are equal, apart from `allowedDifferences`, a map of key to reason:
     `"[Service] Type"` (0054 S8, 0207 D22: the static examples stay `Type=simple`) and
     `"[Service] TimeoutStartSec"` (only `Type=notify` needs it). A key in either set but not the other fails,
     naming the key, the file, and which side has it;
   * **values:** for every key whose template value has no `{{` and is not allowed to differ, the values are equal;
   * **bans:** neither deploy file sets `PrivateDevices` or `RestrictNamespaces`. The message names
     `0099-MADR-installer-service-state-verification.md` F4a and `218/CAPABILITIES`. No value contains `/cache/`;
   * the rendered template itself sets neither banned directive, so the test cannot pass with a regressed template.
4. **`TestDeployRelayUnitNamesEveryRelayVariable`:** collect every `"MCRELAY_[A-Z0-9_]+"` literal in
   `internal/relay/*.go` files not ending `_test.go`, and fail on each one `deploy/systemd/mcrelay.user.service` does
   not name. Also fail if fewer than 30 are found, so a moved package cannot make the test pass with nothing to
   check.
5. **Seen failing, on today's files (C2):** `go test -count=1 -run 'TestDeploy' ./internal/cli/service/`. Expected,
   and quoted in the record:
   * mcrelay: `PrivateDevices`, `RestrictNamespaces`, `GOCACHE` and `GOTMPDIR` only in the deploy file; `USER`,
     `LOGNAME`, `XDG_STATE_HOME` and `XDG_RUNTIME_DIR` only in the template; the F4a ban; the `/cache/` ban;
   * mcremote: the same, less the F4a ban;
   * the relay variable test: `MCRELAY_LIMITS_MAX_CONNS`.

   Any other failure is a fact the MADR missed, and a deviation (C7).
6. Not staged alone. P1 and P2 are one commit, so `master` never has a failing test.

### P2 — The deploy units (MADR D1–D4, D6)

Files: `deploy/systemd/mcrelay.user.service`, `deploy/systemd/mcremote.user.service`.

1. **mcrelay:**
   * delete `:130-132` (the "safe here" comment and the two directives);
   * replace the exclusion comment `:137-141` with the template's block (`mcrelay.user.service.tmpl:73-88`): the five
     absent directives, the 0099 F4a account with its two journal lines, and "do not reintroduce them on that
     reasoning";
   * delete `Environment=GOCACHE=/cache/go` and `Environment=GOTMPDIR=/cache/tmp`;
   * add `Environment=USER=%u`, `Environment=LOGNAME=%u`, `Environment=XDG_STATE_HOME=%h/.local/state` and
     `Environment=XDG_RUNTIME_DIR=%t`, in the template's order;
   * add `# Environment=MCRELAY_LIMITS_MAX_CONNS=…` in the limits block, with the default from
     `docs/guides/config-mcrelay.md`;
   * under `Type=simple`: `# Type=simple, not the template's notify: it works with every release (0054 S8, 0207 D22).
     setup-service installs Type=notify with TimeoutStartSec=240.`
2. **mcremote:** the same `/cache` deletion, the same four environment lines, and the same `Type` comment.
3. **The header comments:** each file's line saying the hardening block must not be omitted stays. The mcrelay
   file's "Manual install" steps stay as they are; they are correct once the unit is.
4. **Checks:**
   * `go test -count=1 -run 'TestDeploy' ./internal/cli/service/`: pass;
   * **seen failing on planted breaks (C2):** in a scratch clone of the repository carrying P1 and P2's working-tree
     files, plant one break per rule, run the two tests there, and expect each break to fail with its own message.
     The test gets no seam for another root, so no test-only variable enters the code:
     * `PrivateDevices=true` put back (the F4a ban);
     * `Environment=GOCACHE=/cache/go` put back (the `/cache/` ban);
     * `KillMode=control-group` in the relay unit (the value rule);
     * a new `ProtectHostname=true` (the key rule);
     * `MCRELAY_LIMITS_MAX_CONNS` removed (the relay variable test).

     The clone is removed afterwards and checked gone.
   * `make verify-units` (`systemd-analyze verify` on all four deploy units);
   * C3's Go gates, and the redact preview.
5. **Stage P1 and P2 together**, with this plan's execution record. The owner commits.

### P3 — The docs (MADR D6, D7)

Files: `docs/guides/ops-mcrelay.md`, `docs/guides/config-mcrelay.md`, `README.md`.

1. **`ops-mcrelay.md`**, beside the manual-unit line (`:83`): a note for a host that installed a manual unit before
   this change.
   * mcrelay: it crash-loops with `218/CAPABILITIES`. Delete `PrivateDevices=true` and `RestrictNamespaces=true` from
     `~/.config/systemd/user/mcrelay.service`, or run `mcrelay setup-service --force`. Then
     `systemctl --user daemon-reload` and `systemctl --user restart mcrelay`.
   * Either manual unit: delete the `GOCACHE=/cache/go` and `GOTMPDIR=/cache/tmp` lines.
   * The check: `systemctl --user is-active mcrelay` reports `active`, and
     `journalctl --user -u mcrelay | grep 218` finds nothing.
2. **`config-mcrelay.md`:** "Most of these … `MCRELAY_LIMITS_MAX_CONNS` is not" goes back to "Every one of these",
   now that the test enforces it.
3. **`README.md`:** the Deploy table's "mirrors embedded unit" names the one difference: `Type=simple`, against the
   template's `Type=notify`.
4. `make check-records`; `make markdownlint-docs`; the redact preview. **Stage.**

### P4 — Acceptance on a real user manager (MADR D8, Q2)

**Gate:** P2 committed. The owner approved the run on 2026-11-08 ("approved to test", MADR Q2).

1. **The script**, `p4_live_unit.py` in the scratchpad, takes the host alias as an argument (no host name in any
   file) and does the following on the Linux test host, under the unit name `mcrelay-0240-check`:
   * copy a scratch build of `mcrelay` (built from the commit under test) and a minimal config, a loopback bind with
     self-signed TLS on a free port, into a scratch directory under `~/0240-check/`;
   * **before:** install the unit as `git show <P2's parent>:deploy/systemd/mcrelay.user.service` had it, with only
     `ExecStart` and the config path pointed at the scratch directory; `daemon-reload`; start it; wait 20 s;
     record `systemctl --user is-active` and the journal lines matching `218|CAPABILITIES`. Expected: `activating` or
     `failed`, with `status=218/CAPABILITIES`. That is the failure seen first;
   * stop it, and install the unit as P2 left it, with the same two substitutions; start it; wait 20 s; record the
     same. Expected: `active`, and no `218` line since the restart;
   * `curl -k https://127.0.0.1:<port>/healthz` returns `{"ok":true}`;
   * stop and disable the scratch unit, delete its file, `daemon-reload`, delete `~/0240-check/`, and check that
     none of them remain.
2. Nothing else on the host is touched: no existing unit, no linger change, no file outside `~/0240-check/` and the
   one scratch unit file.
3. The record quotes both journal excerpts with host details replaced by placeholders (C4).
4. **If the before-run does not fail with 218** on that host, the phase stops. The MADR's F1 rests on 0099's
   reproduction, so a host that cannot reproduce it cannot confirm the fix, and the owner decides what host to use.

**Amended 2026-11-08 (MADR A1, D11).** The first attempt stopped at step 4 (execution record, below). The run repeats
on the same host with Ubuntu's user-namespace restriction turned on. Steps 1 to 4 stand, with these changes:

1a. Before the before-run, the script reads `kernel.apparmor_restrict_unprivileged_userns` and records it. Then it
    runs `sudo -n sysctl -w kernel.apparmor_restrict_unprivileged_userns=1` and checks that the value reads `1`. The
    run is refused if `sudo -n` fails. `/etc/sysctl.d/60-mcremote-userns.conf` is not touched.
1b. In the cleanup's `finally` block, before the unit and directory checks, the script runs
    `sudo -n sysctl -w kernel.apparmor_restrict_unprivileged_userns=<recorded value>` and checks that it reads back.
    The report shows the value before, during and after.
2a. Step 2 gains one exception: the runtime sysctl, restored by 1b. Nothing else on the host changes.
4a. If the before-run still does not fail with 218 with the restriction on, the phase stops again. P4b does not run,
    and the owner decides.

### P4b — Say which hosts fail (MADR A1, D12)

**Gate:** P4 passed with the restriction on: the old unit showed 218, and the fixed one reached `active` and
answered `/healthz`.

1. Replace "every host", "EVERY host" and "never starts" in each place D12 names. The new wording: hosts where the
   user manager cannot create a user namespace with capabilities, which includes stock Ubuntu 24.04 and later.
   * `internal/cli/service/deployunits_test.go`: the `bannedUserUnitDirectives` comment and the F4a `t.Errorf`
     message.
   * `internal/cli/service/mcrelay.user.service.tmpl` and `deploy/systemd/mcrelay.user.service`: the F4a comment,
     the same text in both.
   * `docs/guides/ops-mcrelay.md`: the note's first bullet.
   * `0099-MADR-installer-service-state-verification.md`: the 2026-11-08 note under F4a.
2. `grep -rn 'EVERY host\|every host\|never starts'` over those five files finds nothing.
3. Rerun `p2_planted.py` with its F4a expectation changed to the new message: the `PrivateDevices=true` break must
   fail with it, and the other four breaks are caught as before.
4. `go test -count=1 ./internal/cli/service/`, `make pre-add-check FILES=internal/cli/service/deployunits_test.go`,
   `make race`, `make check-records`, `make markdownlint-docs`, the redact preview. **Stage** with this plan.

### P5 — Close-out

1. The MADR's Confirmation 1 to 5, each with its evidence, in the execution record.
2. The 0239 report's P4 finding gains "Resolved by 0240" with the commit.
3. This plan becomes `complete` and the MADR `accepted`; `check_records.py --write-index`; `make check-records`.
   **Stage.**

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 no banned directive and no `/cache/` path in the deploy user units | P2 | `grep`, and the tests' bans |
| 2 both tests pass, each seen failing first | P1, P2 | P1 step 5; P2 step 4 |
| 3 `verify-units`, `test`, `race`, `pre-add-check`, CI | P2 | C3; CI on the push |
| 4 the user-manager run: 218 before, `active` after | P4 | step 1 |
| 5 the docs note, "every", records and lint | P3, P5 | P3 steps 1 to 4 |
| 6 (A1) no "every host" or "never starts"; the narrowed message seen on a planted break | P4b | steps 2 and 3 |

## Rollout and Rollback

* **Rollout:** the files ship in the repository, not as release assets. A host that copied the old unit keeps it
  until its owner follows P3's note. `setup-service` installs are unaffected: their template was already correct.
* **Rollback:** revert the P1+P2 commit. The tests go with it. P3's note would then describe a fix that is not
  there, so it reverts with it.
* **Risk:** low. The deploy files are documentation-grade examples, read by `make verify-units` and, after this, by
  the two tests. Nothing in the daemon reads them.

## Execution record

The owner answered the MADR's Q1 ("keep simple") and Q2 ("approved to test") on 2026-11-08, and the MADR was
accepted on those answers.

### P0 — 2026-11-08: the records

* This pair, with the answers recorded in the MADR (Owner questions, D4) and here (P4's gate).
* `0099-MADR-installer-service-state-verification.md` gains a dated note under F4a, pointing here.
* `check_records.py --write-index`; `make check-records`; the redact preview.
* **Staged** for the owner's commit: this pair, the 0099 MADR, `docs/README.md`. P1 starts on the owner's word.

P0 was committed by the owner, who approved the plan: "committed, proceed".

### P1 and P2 — 2026-11-08: the tests, then the units

Scripts and logs are in the session scratchpad (`0240-p1-failfirst.log`, `p2_planted.py`, `p2_verify_units.py`,
`p2_verify_remote.py`, `0240-race.log`, `0240-staticcheck.log`, `0240-vettags.log`).

* **Start state (C1).** Clean tree. `master` was two commits ahead of `origin/master` (the owner's 0239 P4 and this
  pair's records), unpushed.
* **P1, the tests.** `internal/cli/service/deployunits_test.go`, `//go:build unix` like `setup_test.go`, since Windows
  installs a Task Scheduler task instead (MADR 0116 D12):
  * `unitDirectives`, the parser;
  * `repoRoot`, which walks up to `go.mod`;
  * `allowedUnitDifferences`, with `Type` and `TimeoutStartSec`, each with its reason;
  * `bannedUserUnitDirectives`;
  * `TestDeployUserUnitsMatchTheTemplates`, one subtest per product: keys, literal values against the raw template,
    the F4a ban on both the deploy file and the rendered template, and the `/cache/` ban;
  * `TestDeployRelayUnitNamesEveryRelayVariable`: 36 names found, with a floor of 30.
* **P1, seen failing on today's files:** `go test -count=1 -run TestDeploy ./internal/cli/service/`, exit 1, with
  exactly the failures step 5 expected and no others:
  * mcrelay, 12: `GOCACHE`, `GOTMPDIR`, `PrivateDevices` and `RestrictNamespaces` only in the deploy file; `LOGNAME`,
    `USER`, `XDG_RUNTIME_DIR` and `XDG_STATE_HOME` only in the template; both F4a bans ("sets PrivateDevices: it makes
    a systemd user unit unstartable on every host (218/CAPABILITIES; 0099-MADR-… F4a)"); both `/cache/` bans;
  * mcremote, 8: the same, less the F4a pair;
  * the relay test, 1: "does not name MCRELAY_LIMITS_MAX_CONNS".

  No literal value differed, so the directives the two files share already agreed.
* **P2, the units,** as steps 1 and 2 say:
  * mcrelay: the two directives and their "safe here" comment are gone, and the exclusion comment is the template's
    0099 F4a block, with a line saying this copy kept them until 0240;
  * both units: the `/cache` lines are gone; `USER=%u`, `LOGNAME=%u`, `XDG_STATE_HOME=%h/.local/state` and
    `XDG_RUNTIME_DIR=%t` are added; a `Type=simple` comment names 0054 S8 and 0207 D22;
  * mcrelay: `# Environment=MCRELAY_LIMITS_MAX_CONNS=1024` is added, with the default from `config-mcrelay.md`.
* **P2, checks:**
  * `go test -count=1 -run TestDeploy`: both pass.
  * **Seen failing on planted breaks** (`p2_planted.py`, in a scratch clone of HEAD with the working tree's three
    files): unplanted, exit 0. Then each break, exit 1, with its own message:

    | Break | Message |
    | :--- | :--- |
    | `PrivateDevices=true` back in mcrelay | "sets PrivateDevices: it makes a systemd user unit unstartable on every host" |
    | `GOCACHE=/cache/go` back in mcremote | "points under /cache/, which is not a standard path" |
    | `KillMode=control-group` in mcrelay | `KillMode = ["control-group"], but mcrelay.user.service.tmpl has ["mixed"]` |
    | `ProtectHostname=true` added to mcrelay | "sets [Service] ProtectHostname, which mcrelay.user.service.tmpl does not" |
    | the `MAX_CONNS` line removed | "does not name MCRELAY_LIMITS_MAX_CONNS" |

    The clone was removed and checked gone.
  * `make pre-add-check FILES=internal/cli/service/deployunits_test.go`: "1 file(s) clean".
  * `go test -count=1 ./internal/cli/service/`: ok.
  * `make race`: exit 0, 49 `ok` lines, no FAIL, panic or race line.
  * `make staticcheck` and `make vet-tags`: exit 0.
  * `make verify-units` is skipped on this Mac (no `systemd-analyze`). So its check ran on the Linux test host
    (systemd 259), on all four deploy units, from a scratch directory: each `systemd-analyze verify` exit 0. The only
    warnings came from the host's own `xfs_scrub` units. The directory was removed and checked gone. The first
    attempt passed the remote script inline, and ssh mangled it; nothing ran on the host, and the script was sent as
    a file instead.
  * The redact preview: 0 files.
* **Staged together**, so `master` never carries a failing test: `deployunits_test.go`, the two deploy user units,
  and this plan.

The owner committed and pushed P1 and P2, and approved P3: "committed, pushed, proceed".

### P3 — 2026-11-08: the docs

* **`ops-mcrelay.md`**, after the manual-unit line: "If you copied a manual unit before MADR 0240", with step 1's
  three parts (the mcrelay 218 fix, by deleting the two lines or by `setup-service --force`; the `/cache` lines in
  either unit; reload, restart and the check). One line says to use `mcremote` in place of `mcrelay` for that unit.
* **`config-mcrelay.md`:** "Every one of these is also listed…", naming the test that keeps it true.
* **`README.md`:** the Deploy row now reads "mirrors the embedded unit (…), except `Type=simple` where the template
  has `Type=notify`".
* **Checks:** `make check-records markdownlint-docs`. The first run failed with two MD004 errors, because the new
  bullets used `*` and the guide uses `-`. After the fix: exit 0, 0 issues in 59 files. The redact preview: 0 files.
* **Deviation, 2026-11-08: the journal check gains `--since "-5min"`.** Step 1 had
  `journalctl --user -u mcrelay | grep 218`. On a host that hit the bug, the journal still holds the old 218 lines,
  so that check would fail after a correct fix. The note runs the check over the last five minutes and says why.
  This changes no decision and no fact in the MADR.

The owner committed P3 (`ffbda60f`) and approved P4: "committed, proceed".

### P4 — 2026-11-08: first attempt, stopped at step 4

Script and logs are in the session scratchpad (`p4_live_unit.py`, `p4_remote.py`, `0240-p4.log`,
`0240-p4-run2.log`). `mcrelay` was built for linux/amd64 with `CGO_ENABLED=0` from `ffbda60f` on a clean tree. The
before unit is `6178ad32^`'s, and the after unit is HEAD's. Each has only `ExecStart`'s binary and config path
pointed at `%h/0240-check/`. The config uses a loopback bind on a free port, `tls.mode: files` with a one-day
self-signed certificate, `data_dir` inside the scratch directory, and one throwaway host entry whose secret was
generated on the host and never printed.

* **Pre-checks (read-only):** `openssl`, `curl` and `python3` present; the user manager reports `running`; no
  `~/0240-check` and no `mcrelay-0240-check` unit.
* **A local pre-flight assertion failed first.** It searched the whole after unit for `PrivateDevices` and matched
  the F4a comment. Nothing had reached the host. It now checks directive lines only.
* **Run 1:** the before unit was `activating (auto-restart)`, `ExecMainStatus=1`. Its journal shows mcrelay itself
  refusing the certificate: "tls file …/cert.pem is readable by group/other". The script had left the certificate
  at mode 0644. No 218 line. systemd had started the process, which a 218 failure prevents.
* **Run 2,** with the certificate at 0600: the before unit reached `ActiveState=active`, `SubState=running`,
  `Result=success`, `NRestarts=0`. No line matched `218` or `CAPABILITIES`. mcrelay logged `mcrelay starting` and
  `listening` on its loopback port.
* **Both runs** stopped as step 4 requires, without running the after unit. Cleanup reported the unit file gone,
  `LoadState=not-found`, and the scratch directory gone. The local stage directory was removed.
* **Why, from read-only probes** (MADR A1, F10): the host has `kernel.apparmor_restrict_unprivileged_userns = 0`
  from `/etc/sysctl.d/60-mcremote-userns.conf`, written by `scripts/bwrap-apparmor-fix.sh` for 0048. `unshare -U
  true` exits 0. The user manager's `CapEff` is `CAP_WAKE_ALARM` alone. `sudo -n true` exits 0.

**Deviation, 2026-11-08: the before-run did not reproduce 218.** Found: the test host has Ubuntu's user-namespace
restriction turned off, and the unit as it was before the fix starts there. Offered: turn the restriction on for the
run on the same host, or use a fresh stock Ubuntu EC2 instance as 0099 did. The owner chose "Flip sysctl on test
host". Recorded in the MADR as A1 (F10, D11, D12) and here as steps 1a, 1b, 2a and 4a and phase P4b. Files added to
scope, in P4b only: `internal/cli/service/mcrelay.user.service.tmpl` (comment only),
`internal/cli/service/deployunits_test.go`, `deploy/systemd/mcrelay.user.service`, `docs/guides/ops-mcrelay.md`
and `0099-MADR-installer-service-state-verification.md`.

The owner committed the amendment (`31e21118`) and approved the rerun: "committed, proceed".

### P4 — 2026-11-08: second attempt, with the restriction on (steps 1a to 4a)

`p4_remote.py` gained steps 1a and 1b. Log: `0240-p4-run3.log`. `mcrelay` was built from `31e21118` the same way.

* **The sysctl:** `0` before; `1` during, read back after `sudo -n sysctl -w`; `0` after, restored in the `finally`
  block and read back (`sysctl_restored: true`).
* **Before unit** (`6178ad32^`): `ActiveState=activating`, `SubState=auto-restart`, `ExecMainStatus=218`,
  `NRestarts=3` after 20 s. Eight journal lines matched, all from the spawn step:

  ```text
  mcrelay-0240-check.service: Failed to drop capabilities: Operation not permitted
  mcrelay-0240-check.service: Failed at step CAPABILITIES spawning /home/<user>/0240-check/mcrelay: Operation not permitted
  mcrelay-0240-check.service: Main process exited, code=exited, status=218/CAPABILITIES
  ```

  These are the lines 0099 recorded on its EC2 instance.
* **After unit** (HEAD): `ActiveState=active`, `SubState=running`, `Result=success`, `NRestarts=0`,
  `ExecMainStatus=0`. No line matched `218` or `CAPABILITIES`. mcrelay logged `mcrelay starting` and `listening`.
* **`curl -sk https://127.0.0.1:<port>/healthz`:** exit 0, `{"ok":true}`.
* **Cleanup:** unit file gone, `LoadState=not-found`, scratch directory gone, local stage removed. A separate ssh
  session afterwards read the sysctl as `0` and found neither `~/0240-check` nor a `0240` unit file.

**MADR A1's hypothesis holds on this host.** With the restriction off, the old unit runs (first attempt). With it
on, the old unit fails with 218. That is the only change between the two runs. The fixed unit runs with the
restriction on.

### P4b — 2026-11-08: the wording

`p4b_wording.py` applied seven edits to five files, each anchor matched once and each result read back:

* `deployunits_test.go`: the `bannedUserUnitDirectives` comment; the F4a message, now "it makes a systemd user unit
  unstartable where user namespaces are restricted, as on stock Ubuntu 24.04 and later (218/CAPABILITIES;
  0099-MADR-… F4a; 0240-MADR A1)"; and the file's header comment, which said the manual install "crash-looped" with
  no qualifier. **Deviation, 2026-11-08:** step 1 named the comment and the message, not the header. The header
  made the same claim, so it was narrowed too. No decision changes.
* `mcrelay.user.service.tmpl` and `deploy/systemd/mcrelay.user.service`: the F4a comment, the same text in both.
  Comment lines only, so no rendered directive changes.
* `ops-mcrelay.md`: the note's first bullet names the hosts that fail, and says a host with the restriction off
  starts the old unit but should still have the lines removed.
* `0099-MADR`: the 2026-11-08 note gains the qualifier.

The source for "stock Ubuntu 24.04 and later" is the repository's existing wording for this restriction:
`ops-linux-install.md` ("AppArmor restricted user namespaces (Ubuntu 24.04+)") and
`0048-MADR-codex-sandbox-namespace.md`.

* **Step 2:** `grep -n 'EVERY host\|every host\|never starts'` over the five files: no output.
* **Step 3:** `p4b_planted.py` (`p2_planted.py` with the new F4a expectation, also copying the template):
  unplanted, exit 0. Then every break exit 1 and caught, the F4a one with the new message: "…sets PrivateDevices:
  it makes a systemd user unit unstartable where user namespaces are restricted, as on stock Ubuntu 24.04 and
  later…". The clone was removed.
* **Step 4:** `go test -count=1 ./internal/cli/service/`: ok. `make pre-add-check
  FILES=internal/cli/service/deployunits_test.go`: "1 file(s) clean". `make race`: exit 0, 49 `ok` lines, no FAIL,
  panic or race line. `make check-records markdownlint-docs`: exit 0, 0 issues in 59 files.
