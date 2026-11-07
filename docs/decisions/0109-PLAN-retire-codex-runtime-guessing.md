---
status: proposed
date: 2026-11-07
associated-madr: "0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md"
---
# Implement the 0109 amendment of 2026-11-07: retire the Codex provider's runtime guessing

Associated MADR: [0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md),
its "Proposed Amendment — 2026-11-07" (D39–D44, evidence E1–E12). This is the record's second plan. The first,
[0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md](0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md),
keeps P8–P17 and is not changed here.

## Goal

The Codex provider decides nothing from codex's error wording, its fixtures say where they came from, a newer codex's
unknown traffic is visible in the logs, and a weekly canary finds a codex release's drift. These close GitHub issue
#31's W1, W2, W4 and W5 as the amendment answers them.

## Scope

**Base.** master at `938b0002` or later. codex-cli 0.166.0 pinned (`KnownGoodVersion`, `testdata/0.166.0/`), and
0.167.0 installed (2026-11-06). Re-pinning to 0.167.0 is
[0236-PLAN-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md](0236-PLAN-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md),
not this plan's work. Wherever this plan says `testdata/0.166.0/`, it means the directory of the pin in force when
the phase runs: `testdata/0.167.0/` once 0236's P2 has landed.

**In scope:**

* `internal/provider/codex/collaboration.go`, `provider.go`: D39's fallback rule, the dual-run, and its later
  removal (P2, P5);
* `internal/provider/codex/testdata/0.166.0/errors/` (new), `testdata/synthetic/` (new) and the two moved fixtures:
  D40 (P1);
* `scripts/capture_codex_errors.py` (new): D40's capture, re-run at each re-pin (P1);
* `internal/provider/codex/provider.go`, `session.go`, `items.go`: D41's once-per-generation Warn (P3);
* `scripts/codex_canary.py` (new), `internal/provider/codex/live_canary_test.go` (new, tag `live_codex_canary`), two
  `Makefile` targets, and the scheduler entry Q-A chooses (P4);
* `docs/guides/ops-codex-contract.md` and a new `docs/guides/ops-codex-canary.md` (P0, P1, P4);
* a dated note in `0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md` D9, and the index (P0);
* the tests named in each phase, and this plan.

**Out of scope:** W3 (not adopted; Q-B); re-pinning to 0.167.0 or later, and persisting the notification split
(`schema_version` 2), both proposed in 0236 (D1 and D3.3); the first plan's P8–P17; any phone change; any GitHub workflow unless Q-A
chooses one.

## Conventions

* **C1. Start state.** Each phase first reads `git status`, `HEAD` and the installed `codex --version`. A difference
  from this plan is a deviation.
* **C2. Seen failing.** Every new test or gate is seen failing first on a scratch copy of the tree, with the planted
  break named in its step, never by dirtying the tree. The handoff says what the failure looked like.
* **C3. Quota.** Only P4's billed canary turn and P6's live suites spend quota. Each runs only on the owner's word at
  that time. The owner's "Weekly" covers the scheduled canary's one turn a week once P4 is accepted, and nothing else.
* **C4. Deviations stop the phase.** They are recorded here as dated entries, and in the MADR when a decision or an
  E-fact changes, before work continues.
* **C5. Gates per phase.** `make pre-add-check FILES=...` on each staged `.go` file, `make test`, `make race`,
  `make vet-tags` when a build tag is added, and `make check-records markdownlint-docs` on record changes.
* **C6. Identifiers.** No hostname, account name or real path in a commit. The disclosure guard's redact preview runs
  from the repository root before anything is staged.
* **C7. Stage only.** The agent stages; the owner commits with `git commit --no-edit`, at the end of each phase.
* **C8. Probes are free.** Every capture runs the installed codex with a scratch `CODEX_HOME`, no login, no model and
  no turn.

## Implementation Steps

### P0 — the records

Files: `0163-MADR-codex-jank-is-stale-pins-not-upstream-churn.md`, `docs/guides/ops-codex-contract.md`,
`docs/README.md`, this plan.

1. On the owner's acceptance of the amendment, the 0109 MADR's banner and section heading say "accepted", with the
   date, and Q-A and Q-B carry the owner's answers.
2. 0163 D9 gains one dated line: superseded in part by the 0109 amendment of 2026-11-07 (D39), which also answers its
   open question 2.
3. ~~`ops-codex-contract.md`'s drift-gate table stops saying the default mode runs in CI (E10).~~ *Done by
   [0236-PLAN-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md](0236-PLAN-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md)
   P4 (2026-11-07), which now says "any host, run by hand; no CI workflow runs it".* What remains here: it names the
   weekly canary (P4) as the place it runs, once P4 lands.
4. `python3 scripts/check_records.py --write-index`; `make check-records markdownlint-docs`.

### P1 — fixtures say where they came from (D40)

Files: `scripts/capture_codex_errors.py`, `internal/provider/codex/testdata/0.166.0/errors/`,
`internal/provider/codex/testdata/synthetic/` with a `README.md`, the tests that read the two moved files,
`docs/guides/ops-codex-contract.md`, this plan.

1. **The capture script.** It starts the codex on `PATH` as `codex app-server` (stdio), with a scratch `CODEX_HOME`,
   and writes the verbatim JSON-RPC `error` objects of:
   * `collaborationMode/list` after an `initialize` without the opt-in (E3): `experimental-gate.json`;
   * an unknown method (E4): `unknown-method.json`;
   * the `initialize` result with the opt-in (E2): `initialize-accepted.json`, its keys only.

   It writes them under `testdata/<codex version>/errors/`, with a `README.md` naming the version, the date and the
   command. It refuses to run if `codex --version` disagrees with `KnownGoodVersion`.
2. **The synthetic fixtures.** `git mv` `testdata/0.147.0/initialize-rejection-experimental.json` and
   `collaborationMode-list-experimental-gate.json` to `testdata/synthetic/`. The README says they were written by
   hand to exercise the `data.capability` branch, and that no codex release has been seen to send that field (E3,
   E6). The tests that read them follow the move.
3. **The other `testdata/0.147.0/` files** share commit `f3698131`, whose message records no capture. They are listed
   in this plan's P1 record, for the owner, and are not moved: D40 names only the two.
4. **The re-pin guide** gains a step after "Set `KnownGoodVersion`": run `scripts/capture_codex_errors.py`, and delete
   the old version's `errors/` with its directory.
5. **Seen failing (C2):** the script run with a scratch `PATH` whose `codex` prints another version refuses and exits
   non-zero; a test that reads a moved fixture from its old path fails on the copy.

### P2 — the `initialize` fallback keys on the code, with the dual-run (D39, D44)

Files: `internal/provider/codex/collaboration.go`, `provider.go`, `collaboration_test.go`, `provider_test.go`, this
plan.

1. **The rule.** `startEngine` (`provider.go:873`) retries once without the opt-in when the `initialize` error is a
   JSON-RPC error with code `-32600`, and logs a Warn naming the code and the codex version. Any other error fails the
   start as today.
2. **The dual-run.** `classifyExperimentalInitRejection` stays, renamed to show it is the old rule, and is called only
   to compare. If the two disagree, a Warn names both answers, the old rule's `via`, the code and the version, but not
   the message. It decides nothing.
3. **D44.** No new reading of codex's message or data text. The `-32601` latches (`collaboration.go:197`,
   `fast_personality.go:147`) are unchanged.
4. **Tests,** each seen failing (C2) on its planted break:
   * E3's captured gate error (P1) as the `initialize` answer restarts once without the opt-in: the rule made to
     return false;
   * a `-32603` does not restart: the rule made to return true;
   * a `-32600` whose message lacks "experimental" logs the disagreement Warn: the comparison removed;
   * the existing `TestExperimentalInitializeRetriesOnce`, `TestUnrelatedInitializeErrorDoesNotRetry` and
     `TestInitializeTransportEOFDoesNotRetry` still pass, or a deviation records why one changed.

### P3 — unknown inbound methods are visible (D41)

Files: `internal/provider/codex/provider.go`, `session.go`, `items.go`, their tests, this plan.

1. **One Warn per kind, method and version, per engine generation**, for:
   * an unknown notification (`routeNotification`'s default, `provider.go:1297-1304`, and the session's default,
     `session.go:1851-1857`);
   * an unknown server request (`provider.go:1322-1344`, `session.go:1878-1889`), after its typed rejection, which is
     unchanged;
   * an unknown item type (`session.go:1589`).

   The log names the kind, the method or item type, the codex version and the sorted top-level key names, at most 16.
   It carries no value. The set of logged keys lives on the engine, so a new generation logs again.
2. **`session.unknownItems`** (`session.go:70`) and its one reader, `item_test.go:288`, are replaced by a test of the
   Warn.
3. **Tests,** each seen failing (C2):
   * each kind logs once, and a second identical arrival does not: the once-only set removed;
   * a new generation logs again: the set moved to the provider;
   * a payload with a value `planted-value-7c1e` never appears in the log: the key-name projection replaced by the raw
     payload.

### P4 — the weekly canary (D42, D43)

Waits for Q-A's answer. The steps below are for the recommended answer (this Mac, `launchd`). A different answer is a
deviation that rewrites this phase before it starts.

Files: `scripts/codex_canary.py`, `internal/provider/codex/live_canary_test.go`, `Makefile`,
`docs/guides/ops-codex-canary.md`, `docs/guides/ops-codex-contract.md`, this plan.

1. **The canary test** (tag `live_codex_canary`): one cheap turn with the codex on `PATH`, which fails on a turn error
   and on no `turn_complete` within its timeout. This is unlike `TestLiveThreadLifecycle`, which passes on a turn error
   (E12).
2. **Make targets.** `live-codex-canary` runs that test alone. `codex-canary` runs the script.
3. **The script.**
   * **Free part:**
     * read npm's latest `@openai/codex` and compare it with `KnownGoodVersion`;
     * `npm install --prefix <scratch> @openai/codex@<latest>`, and put its `bin` first on `PATH`;
     * run `make live-codex-contract` (default mode);
     * record D43's watch list from `codex --help`, `codex app-server --help` and `codex app-server daemon --help`:
       which commands carry `[experimental]`, and whether `acp` exists.
   * **Billed part:** `make live-codex-canary` with `CODEX_HOME` set to the canary home, never the owner's.
   * **The report:** JSON under the canary home's `reports/`, one file per run, holding:
     * the versions, the drift gate's breaking and additive lists, the turn's result, and the watch list;
     * its difference from the last report.
   * **The outcome:** exit non-zero on breaking drift, a failed turn, or a watch-list change, with a macOS notification
     saying which.
   * `--free-only` skips the billed part. The script refuses to run the billed part if the canary home has no login.
4. **The schedule.** `scripts/codex_canary.py --install-launchd` writes one user `LaunchAgent` that runs it weekly,
   and `--uninstall-launchd` removes it. The canary home and the agent live in the owner's home directory, outside the
   repository. The owner signs the canary home in once with `codex login`. The agent never copies a credential.
5. **The guide** `ops-codex-canary.md`: what the canary runs, what it spends, where the reports are, how to sign in, how
   to install and remove it, and what to do with each failure kind.
6. **Seen failing (C2), all free:**
   * the free part against a scratch `PATH` codex shim whose schema export lacks a pinned method exits non-zero and
     names it;
   * a planted watch-list change (a shim whose `--help` drops `[experimental]` from `generate-json-schema`) exits
     non-zero;
   * the canary test against a shim that answers the turn with an error fails.
7. **The first runs.** `--free-only` against npm's latest (0.167.0 today), whose report is recorded here (Confirmation
   7). The first billed run, and the `launchd` install, only on the owner's word.

### P5 — the dual-run ends (D39)

Starts only after both of these:

* the first codex re-pin after P2 lands has been accepted, under its own plan;
* the owner confirms that the daemon logs on the hosts where codex is used show no disagreement Warn since P2.

A disagreement found is a deviation: the rule or D39 is revisited before anything is deleted.

Files: `internal/provider/codex/collaboration.go`, `collaboration_test.go`, `testdata/synthetic/`, this plan.

1. Delete the old classifier, `experimentalCapabilityRe` and the comparison, and the synthetic fixtures and tests
   that served only the old rule.
2. Confirmation 1: `rg '\(\?i\)experimental' internal/provider/codex --glob '!*_test.go'` finds nothing.

### P6 — acceptance

1. `make test`, `make race`, `make vet-tags`; `make ci-windows` on the Windows dev host, since the package builds
   there.
2. `make live-codex-contract` (free) against the pin.
3. `make live-codex` and `make live-codex-turn`, on the owner's quota word: the provider is unchanged for a user.
4. The amendment's Confirmation 1 to 7, each with its evidence below.
5. A comment on issue #31 naming what closed each workstream. The issue closes on the owner's word.
6. Status: this plan becomes `complete`, or `in-progress` with P5 left, which waits on a re-pin.

## Verification

| Confirmation | Phase | Where |
| --- | --- | --- |
| 1 no prose match left | P5 | step 2 |
| 2 a `-32600` restarts once, a `-32603` does not | P2 | step 4 |
| 3 the disagreement Warn | P2 | step 4 |
| 4 captured and synthetic fixtures apart | P1 | steps 1 and 2 |
| 5 one Warn per unknown, with no value | P3 | step 3 |
| 6 the canary fails on drift and passes on the pin | P4 | step 6 |
| 7 the first canary report (0.167.0) | P4 | step 7 |

## Rollout and Rollback

* P1 to P3 are internal to the provider. Rolling back a phase is reverting its commit. No configuration, protocol or
  phone change.
* P4 adds a scheduled job outside the repository. `--uninstall-launchd` removes it, and the canary home can then be
  deleted. Nothing in the daemon depends on it.
* P5 is the only step that removes a fallback, and it waits for log evidence and a re-pin.
