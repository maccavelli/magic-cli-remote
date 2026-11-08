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
