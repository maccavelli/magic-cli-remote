---
status: complete
date: 2026-11-05
associated-madr: "0232-MADR-redact-real-zones-hostnames-and-ips-and-deny-them.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement redacting the real zones, hostnames and IPs, then denying them

Associated MADR: [0232-MADR-redact-real-zones-hostnames-and-ips-and-deny-them.md](0232-MADR-redact-real-zones-hostnames-and-ips-and-deny-them.md)

## Goal

1. No tracked file holds any F1 value (MADR D1, D2).
2. Every test asserts what it asserts today.
3. Four deny rules refuse a new mention, and refuse nothing on the day they land (D3).
4. Issue #35's body names no host (D4).

## Scope

**In:**

* every file F1 lists. P1 step 2 fixes the exact list from the scan, and nothing outside it is
  edited;
* four lines appended to the global deny list (P2);
* issue #35's body (P3);
* this pair.

**Out:**

* published history (D5);
* the guard tool;
* the values F1 keeps, for the reasons F1 gives;
* a committed identifier check (owner question 2).

## Constraints

* The pair is committed on its own, before the redaction. The redaction commit holds only the
  redacted files.
* P2 waits until `master` is level with `origin/master`. Before that, the rules would refuse the
  push that carries 0231's commits and P1's commit (F3).
* The agent stages, and the owner commits and pushes. The deny list and the issue are changed only
  with the owner's word in that turn.
* No record, commit message or quoted log spells an F1 value. The values exist only in the scratch
  script, the scratch rule list and the global list.

## Implementation Steps

### P0 — The records

1. Present this pair, and get the owner's approval and the answers below.
2. `check_records.py --check-all` exits 0. Then the redact preview, the identifier scan, and a
   search of the pair for every F1 value (0 hits).
3. Stage the pair. The owner commits. The index waits for the owner's `docs/README.md` edits, as in
   0231.

### P1 — The redaction commit (D1, D2)

1. **The script**, `<scratchpad>/0232/redact.py`, holds the six D1 replacements.
   * The relay alias matches as a whole word. The Linux alias is replaced only at its 18 positions
     (MADR F1), each listed by file and line and asserted to hold the word. The English uses in
     0086-MADR and 0116-MADR, and the opencode fixture's model name, are untouched.
   * The zone is replaced before the aliases, so `<alias>-<zone>` becomes
     `linux-host-example-com`.
   * It is proven first on a scratch file that holds each form, and a model name it must leave
     alone.
2. **Dry run** in the tree. The script prints each file with its per-pattern counts. They must
   equal the class scan's counts for F1, file by file. A difference is a deviation: stop and
   prompt.
3. **Apply.** Then:
   * the class scan, re-run, finds none of the values;
   * the guard's `redact` with `DISCLOSURE_DENY=<scratch D3 list>` reports "would edit 0 file(s)";
   * `git diff --stat` lists the dry run's files only;
   * `git diff` is read in full. Every changed line differs only by a D1 replacement.
4. **Formatting.** `gofmt -l` on the Go files prints nothing. Markdown tables are re-padded only
   where markdownlint reports them.
5. **Gates:**
   * `make pre-add-check FILES=` the redacted Go files;
   * `go test -race -count=1` on `./internal/config/`, `./internal/cli/`, `./internal/daemon/` and
     `./internal/provider/codex/`;
   * `make race`;
   * `check_records.py --check-all`;
   * markdownlint over the README and the guides.

   The edits are literals, with no build tag, path or OS behaviour, so host runs are not required.
   The record says so.
6. **Seen failing (scratch copy):** restore the old zone in `config_test.go`'s `PrimaryDomain`
   check only. The test must fail, which shows the fixture and the assertion were both changed.
7. Stage the files and this PLAN with its P1 record. The owner commits and pushes.

### P2 — The rules (D3)

1. **Preconditions,** each recorded:
   * `git status -sb` shows `master` level with `origin/master`;
   * the clone scan, re-run with the four rules, finds no other GitHub clone holding any of them.
2. With the owner's go-ahead: copy the list to `disclosure-deny.bak-0232`, check the copy with
   `cmp`, then append the four lines. The agent reads only the line count, 35 before and 39 after.
3. **Proof:**
   * the guard's `pre-push`, run read-only over `HEAD` against `origin/master`, exits 0;
   * in a scratch clone, one commit holding one planted string per rule is refused with four
     hits. Then the clone is removed.

### P3 — Issue #35 (D4)

1. With the owner's word: fetch the body, replace the relay's hostname with `<relay-host>`, and
   show the owner the diff.
2. `gh issue edit 35 --body-file <scratch>`. Fetch it again, and confirm 0 matches for the zone
   and the alias.
3. Record P1 to P3. The PLAN goes to `complete`.

## Verification

| # | Criterion | Phase |
| :--- | :--- | :--- |
| V1 | The class scan finds no F1 value in any tracked file | P1 |
| V2 | Only the dry run's files changed, and only by D1's replacements | P1 |
| V3 | The four packages' tests and `make race` pass, and the planted revert fails | P1 |
| V4 | The guard passes the real outgoing commits and refuses each planted string | P2 |
| V5 | No other GitHub clone is blocked on the day the rules land | P2 |
| V6 | Issue #35's body names no host | P3 |

## Rollout and Rollback

* **Rollout:** P0, P1 (the owner pushes), P2, P3.
* **Rollback:** P2, restore `disclosure-deny.bak-0232`. P1, `git revert`, which republishes the
  values, so it is the owner's call alone. P3, GitHub's body history.

## Owner decisions (answered 2026-11-05)

1. **Approve the pair**, with D1's replacements.
2. **A committed identifier check** (MADR Consequences): a script and CI job that refuse public
   IPv4 and IPv6 addresses outside the documentation ranges, and domain names outside an
   allowlist, in tracked files. It would catch an identifier no rule lists yet. It needs its own
   pair; say whether to write one.

The owner: "D3. not at this time. i committed and pushed, proceed". Question 3 of the handoff was
question 2 here: the committed identifier check is not written now. "Proceed" approves the pair
and D1's replacements. P2 and P3 still need the owner's word in their own turns.

## Execution record

### P0 — The records (2026-11-05)

* **Step 1.** Presented. Answers as above.
* **Step 2.**
  * `check_records.py --check-all`: exit 0.
  * The identifier scan over the pair: "hits: 0".
  * `le/guard_text.py` checks text with the guard's own `load_rules` and `scan_text`, and with
    this session's identifier patterns. It was first seen failing on a planted file, with one
    `<user>` hit and two session hits. Over the MADR and this PLAN it reports 17 rules, 0 hits,
    and 0 session hits.
* **Corrected before the commit.** Reading each of the Linux alias's 20 whole-word matches in
  context showed that two are the English word: a cache idiom in 0086-MADR line 291, and a verb
  in 0116-MADR line 1619. F1's row was 20 lines in 10 records and 1 report. It is now 18
  occurrences in 8 records and 1 report, with those two named as kept. D2, and P1 step 1, now
  replace the alias only at its 18 listed positions, not by a whole-word pattern.
* **Step 3.** Staged: this pair, with
  [0231-PLAN-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md](0231-PLAN-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md)'s
  record of posting on issue #35. These are records only, under the bootstrap exception. The
  index is still deferred.

**Files for this commit:** this pair and the 0231 PLAN.

### P1 — The redaction commit (2026-11-05)

Scripts and logs are in the session scratchpad, under `0232/`.

* **Step 1, the script** (`redact.py`). It has five pattern rules: the zone in either form,
  case-insensitive, with the separator kept; each IPv4 with digit and dot boundaries; the relay
  alias as a whole word; and the installation ID as a literal. The Linux alias is handled by an
  explicit table of 18 `(file, line)` positions, each asserted to hold the word exactly once. The
  two English uses are listed as kept.
  * `redact.py prove` on sample text: every form was replaced, including an upper-case one. A
    longer address that contains the relay's, a longer word that contains the alias, the model
    name, the English uses and an unlisted line holding the alias were all left alone. "proof
    ok".
  * The first proof run stopped on the one-per-line assertion, because the sample put two aliases
    on one line. None of the 18 real positions does. The sample was corrected; the assertion was
    not.
* **Step 2, dry run:** 30 files and 130 lines. Totals: zone 111, relay IP 4, droplet IP 2, relay
  alias 10, installation ID 1, Linux alias 18. Each equals MADR F1. The set of changed
  `(file, line)` pairs equals the set that `git grep` finds independently for each pattern, minus
  the two kept English lines and the opencode fixture: "checks pass".
* **Step 3, apply.**
  * `git grep -i` for every F1 value returns nothing. The only remaining whole-word matches for the
    Linux alias are the two kept English lines.
  * `git diff --stat` lists the 30 files, plus the owner's own uncommitted `docs/README.md` and
    0179 pair, which this phase did not touch.
  * `verify.py`: each of the 30 files equals `redact_text` of its `HEAD` version, byte for byte:
    "30 files checked, 0 differ". Seen failing: its `selftest` mode adds one extra edit in memory
    to `config_test.go`, and the check reports "1 differ".
  * The word diff was read in full. Every change is a D1 replacement, and each reads correctly in
    place. The headscale guide still names a VPC resolver at a private address. That address is
    not reachable from outside and is not one of F1's values, so it is kept.
  * The guard's own `redact`, with `DISCLOSURE_DENY` set to a scratch list of D3's four rules,
    reports "would edit 0 file(s)" on the tree. In a scratch repository planted with all four
    values, the same command reports "would edit 1 file(s)", with D1's four replacements. The
    scratch repository was removed.
* **Step 4.** `gofmt -l` on the four Go files prints nothing. markdownlint needed no re-padding.
* **Step 5, gates** (logs `0232/*.log`):

  | Gate | Result |
  | :--- | :--- |
  | `make pre-add-check FILES=` the four Go files | exit 0, "4 file(s) clean (gofmt, golint, govulncheck)" |
  | `go test -race -count=1` over `internal/config`, `internal/cli`, `internal/daemon`, `internal/provider/codex` | exit 0, four `ok` |
  | `make race` | exit 0; 48 `ok`, no FAIL, panic or race |
  | `check_records.py --check-all` | exit 0 |
  | markdownlint over `README.md` and the three guides | "0 issues" |

  No host runs: the edits are string literals and help text, with no build tag, path or OS
  behaviour.
* **Step 6, seen failing** (`0232/plant_p1.py`, on a scratch copy that was removed after): with the
  old zone put back in `TestTLSValidateAccepts`' `PrimaryDomain` comparison only, the test fails
  with `config_test.go:469: primary="devbox.ts.example.com"`. So the fixture and the assertion
  were both redacted.
* **Step 7.** Staged after the pair's commit (`8992071d`) landed, so the commits stay separate: the
  30 files and this PLAN. `verify.py`, re-run just before staging, again found each of the 30 files
  byte-equal to its redacted `HEAD` version. The owner commits and pushes. P2 waits for `master` to
  be level with `origin`, and for the owner's word.

**Files for this commit:** the 30 redacted files and this PLAN.

#### Deviation, 2026-11-05: the commit message named both aliases

* **Found.** The owner's push was refused. The hook-written message of the P1 commit listed both
  real aliases in parentheses on its fifth line. The guard blocked it on the Linux alias (MADR F4).
  The commit's files were clean, and the pair's commit passed.
* **Chosen** by the owner, of three offered (the owner rewords it; the agent amends this once;
  regenerate through the hook): "I amend, this once".
* **Done.** `0232/reword.py` wrote the message with that one line changed, asserted to be the
  only line that differs. The index matched `HEAD`. `git commit --amend -F` gave `2ad168ff`, and
  `git diff --quiet` shows its tree equal to the refused commit's. The guard's `pre-push` passed
  both outgoing commits (exit 0). `le/guard_text.py` found 0 hits in both messages, and on the
  original message it reported both aliases. The owner then pushed.

### P2 — The rules (2026-11-05)

Script: `0232/p2.py`.

* **Step 1, preconditions.** After `git fetch`, `HEAD` equals `origin/master`. The clone scan
  applied the four rules to every tracked text file of every clone under `~/gitrepos` with a
  GitHub remote: 0 clones hold a D3 value. Seen failing first: over a scratch root holding one
  planted clone with a GitHub remote, it reported that clone, with hits for the zone and the relay
  IP. The scratch root was removed.
* **Step 2.** On the owner's "pushed proceed": the list was copied to `disclosure-deny.bak-0232`
  and checked equal byte for byte, then the four lines were appended. Line count 35 → 39; the
  guard loads 21 rules, which was 17.
* **Step 3, proof.**
  * Changed from the step as written: with `master` level, `HEAD` against `origin/master` has no
    commits to scan. The guard's `pre-push` was run over `HEAD` against `HEAD~1` instead. That
    scans `HEAD`'s full tree and message, a stricter check. Exit 0.
  * A scratch clone, with one commit holding one planted string per rule: `pre-push` exit 1, with
    four `BLOCKED` lines, one per rule (placeholders `example\1com`, `203.0.113.10`,
    `203.0.113.20` and `relay-host`). The clone was removed.
* **Also probed** (MADR F4): how the existing rules treat the Linux alias, with the guard's
  `scan_text` on sample lines.

### P3 — Issue #35 (2026-11-05)

Script: `0232/issue35_redact.py`.

* **Step 1.** The fetched body holds the relay's full hostname twice: in the setup line, and in
  the quoted ACME error. Both became `<relay-host>`, and the replaced text was asserted to hold
  neither the zone nor the alias. The diff is those two lines. `le/guard_text.py` found 0 hits on
  the new body. On the old body it found 4 under the guard's rules (21 by then) and 4 session
  identifiers.
* **Step 2.** `gh issue edit 35 --body-file`. The body fetched again equals the intended body, plus
  the one trailing newline that `jq` adds on output. It holds 0 identifiers. The issue's title and
  comments hold 0 too, and it is still closed. GitHub keeps the previous body in the issue's edit
  history, which only the owner can delete, from the web.
* **Step 3.** This record. The PLAN is complete. The records index waits for the owner's
  `docs/README.md` edits, as in 0231.

| # | Result |
| :--- | :--- |
| V1 | P1 step 3: 0 values in the tree |
| V2 | P1 steps 2 and 3: the dry run's 30 files, D1 replacements only |
| V3 | P1 steps 5 and 6 |
| V4 | P2 step 3 |
| V5 | P2 step 1 |
| V6 | P3 step 2 |

**Files for this commit:** this pair.
