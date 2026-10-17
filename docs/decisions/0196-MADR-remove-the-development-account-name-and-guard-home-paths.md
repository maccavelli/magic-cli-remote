---
status: proposed
date: 2026-10-17
decision-makers: Project Owner
consulted: none
informed: contributors who write tests, fixtures or records with paths in them
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Remove the development account name from committed files, and guard every tracked file's home paths

## Context and Problem Statement

`AGENTS.md`, "Host identifiers never appear in records": "This repository is public. Nothing
committed carries a hostname, account name, or absolute path with a real user in it. Use
placeholders". The global rules say the same for every committed file, and that an existing
redaction is a standing instruction.

A read-only survey on 2026-10-17 found a real account name in tracked files. The survey listed the
user segment of every home-directory path (`/home/<seg>`, `/Users/<seg>`, `C:\Users\<seg>`) in
`git ls-files`.

* **One account name appears 104 times, in 28 files.** It is the account on a development host,
  written here as `<dev-account>`. 0109's probe
  evidence shows it as the Linux test host's codex install path, for example. Where it appears:
  * Go tests (`internal/cli/service/setup_test.go`, `plist_render_test.go`) and Dart tests
    (`apps/mobile/test/settings_store_test.dart`, `chat_back_arrow_test.dart`), as an example home.
  * Codex probe fixtures (`internal/provider/codex/testdata/codex-spike-0.145.0/summary.json`,
    `summary2.json` and `item-stream-raw.jsonl`), as captured.
  * Two probe scripts, `scripts/quick-probe.py` and `scripts/probe-codex-item-stream.py`, as
    `REPO = Path("/home/<dev-account>/gitrepos/magic-cli-remote")`. That also means neither script runs from
    any other checkout.
  * Nineteen records (15 under `docs/`, 4 under `apps/mobile/docs/`), in probe evidence and as a
    "standards" path (`/home/<dev-account>/standards/...`).
* **No other real identifier was found.** The other accounts and host names known to the owner do
  not appear in tracked files. Every other segment in the survey is a placeholder: `user` (642),
  `alice` (36), `REPLACE_ME` (27), `dev` (22), `me` (15), and a tail of test values (`x`, `proj`,
  `sentinel`, `testuser`, `Zoë` and others).
* **Every occurrence is already pushed.** The global rules: "Already pushed: report the commits and
  files; do not rewrite published history unasked."
* **The existing guard is narrow.** `TestCommittedFixturesCarryNoIdentifiers`
  (`internal/wirecap/fixtures_test.go`) reads only `*/testdata/wire/*/frames.jsonl`, and looks only
  for email addresses and Windows drive paths. Nothing checks a home path's account anywhere else.
  So the 104 occurrences went unnoticed, and the agent identifier scans each session ran covered
  only the lines that session added.

### The question

How should Magic remove the account name it has, and stop a real account name reaching a committed
file again, without publishing the names it means to keep out?

## Decision Drivers

* The rule is already decided: no account name in a committed file. This record is about doing it
  and keeping it done.
* A guard must not publish what it guards against. A list of real names in a public test is itself
  the leak.
* A guard must run where tests run (`go test`, CI and the Windows host), not only in an agent's
  session.
* Rewriting published history is destructive, and is the owner's call alone.

## Considered Options

* Replace the name, and guard with an allowlist of placeholder segments
* Replace the name, and guard with a list of banned names
* Replace the name, and guard with hashes of banned names
* Replace the name, with no guard

## Decision Outcome

Chosen option: "Replace the name, and guard with an allowlist of placeholder segments". It is the
only guard that catches an account name nobody has listed yet, and it names no real account to do
it.

* **R1. Replace `<dev-account>`.**
  * In records and fixtures: `<user>`, as `AGENTS.md` asks.
  * In Go and Dart tests: `user`. These values pass through code, such as plist and unit
    rendering, path joins and recent-folder lists. `user` keeps them ordinary paths, and it is
    already the repository's most common placeholder.
  * The probe evidence in records keeps its meaning: only the account segment changes.
  * A record's history is not rewritten. Each changed record gains a one-line dated note that the
    account was redacted by this record.
* **R2. The two scripts find the repository themselves:**
  `REPO = Path(__file__).resolve().parents[1]`. That removes the name, and makes them run from any
  checkout.
* **R3. A guard over every tracked file.** `TestTrackedFilesUseOnlyPlaceholderHomes`, in the
  `internal` package beside `TestGoModRequiresTaggedACPDirectly`:
  * It lists tracked files with `git ls-files`, and fails if `git` is missing or lists nothing, so
    it cannot pass by reading nothing.
  * It reads each UTF-8 text file. It extracts the user segment of every `/home/<seg>`,
    `/Users/<seg>` and `C:\Users\<seg>` path, raw or JSON-escaped. The segment ends at a
    separator, whitespace, quote, bracket or `.`.
  * The segment must be in an allowlist of placeholders: the survey's set, plus any placeholder
    in angle brackets such as `<user>`.
  * A failure names the file, the line and the segment. It says to use a placeholder, or to add
    one to the allowlist if the segment is not a real account.
  * Adding a placeholder is a one-line change, visible in review. A real name would have to be
    added on purpose, in plain sight.
* **R4. History is not rewritten.** The name stays in published commits. Rewriting `master`
  would change every commit hash since the first occurrence, and break every clone. If the owner
  wants that, it is a separate decision.
* **R5. Scope.** The guard covers home paths only. Host names, email addresses beyond the wire
  fixtures, and other identifiers stay with the agent scans and review. A host name has no shape a
  pattern can find without listing it.

### Consequences

* Good, because the account name leaves the current tree, and the guard stops any new account name
  in a home path, listed or not.
* Good, because the guard publishes only placeholders.
* Good, because the two probe scripts work from any checkout.
* Neutral, because a new test that uses a new placeholder must add it to the allowlist.
* Bad, because the name stays in history (R4).
* Bad, because the guard sees home paths only. An account name written any other way, such as
  `user@host` text or a bare word, is not caught (R5).

### Confirmation

* After R1 and R2, the survey script finds no segment outside the allowlist, and
  a `git grep` for `<dev-account>` in home paths finds nothing.
* The guard passes on the tree. On a scratch copy it fails for each of:
  * a tracked file with a non-placeholder segment, naming the file, line and segment;
  * a JSON-escaped `C:\\Users\\<seg>` form;
  * a `git ls-files` that lists nothing.
* Every test that used `<dev-account>` passes with `user`, including `make preflight` for the Dart files.
* `make ci-windows` passes, so the guard's `git` use and path handling work on Windows.

## Pros and Cons of the Options

### Replace the name, and guard with an allowlist of placeholder segments

* Good, because it catches names nobody listed.
* Good, because the committed list holds only placeholders.
* Bad, because new placeholders need an allowlist line.

### Replace the name, and guard with a list of banned names

* Good, because it never flags a new placeholder.
* Bad, because the list is itself a committed copy of the names. In a public repository it
  publishes exactly what it guards.
* Bad, because it misses every account not on the list.

### Replace the name, and guard with hashes of banned names

* Good, because the names are not written out.
* Bad, because a short account name is recovered from its hash by trying a dictionary of names in
  seconds, so this hides little.
* Bad, because it still misses every account not on the list.

### Replace the name, with no guard

* Good, because it is the least work.
* Bad, because this is how the 104 occurrences built up: per-session scans of added lines did not
  see what was already there.

## More Information

* The survey script is a scratch file, not committed. R3's test is its committed form.
* `TestCommittedFixturesCarryNoIdentifiers` stays. It covers email addresses and Windows drive
  paths in the wire fixtures, which R3 does not.
* The PLAN is
  [0196-PLAN-remove-the-development-account-name-and-guard-home-paths.md](0196-PLAN-remove-the-development-account-name-and-guard-home-paths.md).
  Nothing is implemented until the owner accepts an option and approves the PLAN.
