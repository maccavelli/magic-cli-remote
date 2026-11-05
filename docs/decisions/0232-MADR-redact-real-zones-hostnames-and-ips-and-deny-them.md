---
status: accepted
date: 2026-11-05
decision-makers: Project Owner
consulted: "this repository at 09d82a8c (every tracked text file, scanned by class); ~/.global-git-hooks/github-disclosure.py, read 2026-11-05; the global deny list's line count only; the local clones under ~/gitrepos with a github.com remote; issue #35's body; 0171-MADR-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md"
informed: anyone who writes examples, fixtures, guides or records in this repository, or edits the global disclosure deny list
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Redact the real zones, hostnames and IPs from the tree, then deny them at push time

## Context and Problem Statement

While 0231's live acceptance was running
([0231-PLAN-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md](0231-PLAN-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md),
P4), the agent found that `docs/guides/iam-route53-acme.md` uses the owner's real zone in its
worked examples. A scan of every tracked text file then found the zone in 20 files, and found
other real identifiers beside it. The scan covered public IPv4 and IPv6 addresses, domain names,
Route 53 zone IDs, AWS account IDs and ARNs, EC2 and tailnet hostnames, host aliases, e-mail
addresses, account names and home paths.

The repository is public, and AGENTS.md says that nothing committed carries a hostname. The
owner, 2026-11-05: "Redact any identifying data including real zones hostname and ip. How was
all of this allowed through?"

### F1. What the tree holds (scan at `09d82a8c`)

| Class | Where | Lines | What it is |
| :--- | :--- | ---: | :--- |
| The owner's zone, dotted and hyphenated | 20 files: 4 Go files (3 tests, the `--help` examples), 3 example configs, the README, 3 guides, 9 records | 103 | fixture values, worked examples, the production relay's URL, a droplet's name |
| The relay's public IPv4 | `docs/guides/headscale.md`, `0130-PLAN` | 4 | the relay host's Elastic IP |
| A droplet's public egress IPv4 | `0098-PLAN` | 2 | the workstation droplet's address |
| The relay host's alias | 4 records (0097-MADR, 0097-PLAN, 0098-MADR, 0098-PLAN), `docs/guides/headscale.md`, a codex fixture | 10 | the SSH alias, and an MCP server named after it |
| The Linux test host's alias | 8 records and 1 report (0098, 0100, 0160, 0161, 0162) | 18 | the SSH alias, and a droplet's name. Two more whole-word matches, in 0086-MADR and 0116-MADR, are the English word and are kept |
| A codex installation ID | the same codex fixture (`codex-spike-0.145.0/item-stream-raw.jsonl`) | 1 | the owner's codex install, beside the server name |

* No test resolves, dials or certifies any of these. Each is an opaque string. No Go code reads
  the fixture's `serverName` or `installationId`.
* Issue #35's body, which the owner wrote, names the relay's full hostname twice.
* **Not identifying, and kept:**
  * `100.64.0.x`: headscale's default first allocations, the same on every tailnet;
  * `Z0123456789ABCDEFGHIJ`: the guide's made-up zone ID;
  * `1.1.1.1`: a public resolver;
  * dotted quads that are version numbers (`25.0.4.1`, `9.9.9.1`, …);
  * `module@version` strings that look like e-mail addresses;
  * `mcremote.dev`: the receipt format's type URIs (`internal/receipt/statement.go:14`). These are
    protocol constants, and changing them would break every signed receipt;
  * the repository's own GitHub owner in its URLs;
  * a model's name in an opencode fixture, which contains the Linux alias as an ordinary English
    word.
* No account name, home path or real zone ID is in the tree. The guard's existing rules already
  hold for those: its `redact` preview edits nothing.

### F2. How it got through

* **The push guard is a deny list.** `github-disclosure.py` refuses only what a rule in
  `~/.config/git/disclosure-deny` matches. The list has 35 lines. It has no rule for the zone,
  either IP, or either host alias, and no generic detector for IPs or domain names. Everything in
  F1 passed every push.
* **The agent-side scan is narrow, and lives in a session scratchpad.** The `id_scan.py` that
  0214 and 0231 ran looks for the local account name, the Linux and Windows test-host aliases,
  home and temp paths, and one mail domain, and only in added lines. It has no pattern for domain
  names, IPs or the relay alias. Its "hits: 0" was accurate for those patterns, and was reported
  as if the records were clean of identifiers.
* **The rule came after the first identifiers.** The zone and the relay alias first came in with
  `be10760d` (2026-07-19). AGENTS.md's "Host identifiers never appear in records" came with
  `a59b9ad3` (2026-09-21). Later records (0098, 0115, 0130, 0142, 0162) quoted live evidence
  with the real names, and the rule is prose that nothing enforces.
* **Issue text is never scanned.** The guard sees only pushed commits.
* **CI has no identifier gate.**

### F3. The guard's behaviour, which constrains the order

* `pre-push` scans the whole tree of every outgoing commit (`tree_locations`, `git ls-tree -r
  --full-tree`, lines 187-205). While any outgoing tree holds a denied string, every push is
  refused.
* The list is global, so a rule applies to every repository that pushes to GitHub. Of the 35
  clones with a GitHub remote, only this one holds the zone. The other rules' strings are
  re-checked in P2.
* 0171-MADR did this once already: one redaction commit, neutral valid values for fixtures, no
  history rewrite.

## Decision Drivers

* Nothing a reader can reach names the owner's zones, hosts or addresses (AGENTS.md; the owner).
* Every test asserts what it asserts now.
* No published history is rewritten. The old text is public through the history and DNS.
* The guard never refuses a push the owner did not expect it to refuse.
* A recurrence is stopped mechanically. The prose rule did not stop one.

## Considered Options

* **A** — Redact every F1 item in one commit, then add deny rules for each one that a regex can
  match without false hits. Edit the issue body on the owner's word.
* **B** — Redact the zone only (the scope before the owner's widening).
* **C** — A, and also rewrite the published history.

## Decision Outcome

Chosen option: **A**, by the owner's instruction of 2026-11-05. Before that, the owner had chosen
the zone's whole tree and a deny rule, "Whole tree, then deny list (Recommended)". The
instruction widens that to every identifying class.

### The decisions

* **D1 — Replacements**, each reserved or neutral, and each keeping the reader's sense:

  | F1 item | Becomes |
  | :--- | :--- |
  | the zone (dotted / hyphenated) | `example.com` / `example-com` (RFC 2606). Labels are kept, so `<relay>.example.com` still names the relay |
  | the relay's IPv4 | `203.0.113.10` (RFC 5737 TEST-NET-3) |
  | the droplet's IPv4 | `203.0.113.20` |
  | the relay host's alias | `relay-host`, in prose and in the fixture |
  | the Linux test host's alias | `linux-host` |
  | the codex installation ID | `00000000-0000-0000-0000-000000000000` |

* **D2 — One redaction commit**, made by one scratch script. The script holds the six
  replacements as exact patterns, asserts each file's expected count before it writes anything,
  and replaces the Linux alias only at its 18 listed positions. So the English word, and the
  opencode fixture's model name, are untouched. The commit holds the redacted files and nothing
  else. The guard's `redact` preview, pointed at a scratch list of D3's rules, must then report
  0 files to edit.
* **D3 — Deny rules, added last:** four, one each for the zone (both forms), the relay's IPv4, the
  droplet's IPv4 and the relay alias.
  * The Linux alias gets no rule. It is an English word, and part of a model's name in a
    fixture, so a rule would refuse legitimate text. The agent-side scan already looks for it.
  * The rules go into the global list only after these hold: the redaction commit exists; every
    earlier commit whose tree holds one of the strings is on `origin` (F3), which includes
    0231's unpushed commits; and a re-run of the clone scan finds no other GitHub clone holding
    any of the four strings.
  * After the rules are added, the guard runs read-only over the outgoing commits and must report
    0 hits.
* **D4 — Issue #35's body:** the relay's hostname becomes `<relay-host>`, on the owner's word,
  with `gh issue edit`. GitHub keeps the edit history of the body. Only the owner can delete
  that, from the web interface.
* **D5 — No history rewrite** (C rejected), as 0171-MADR.

### Consequences

* Good, because nothing a reader can reach names the owner's infrastructure, and four of the
  identifiers are refused in any repository from then on.
* Good, because every test keeps its meaning with reserved values.
* Neutral, because examples in `--help` and the README show `example.com`.
* Bad, because a deny list only knows what it lists. A new identifier, such as a new host or
  address, is not caught until a rule names it. A committed check that knows the classes, not the
  values, would close that gap. It is a separate decision, offered in the PLAN.
* Bad, because records now cite `example.com` hosts and TEST-NET addresses. The meaning is kept,
  and the history holds the originals.

### Confirmation

* After P1, the class scan reports none of F1's values in the tree, and the guard's preview
  against a scratch list of D3's rules reports "would edit 0 file(s)".
* `make pre-add-check` passes on the redacted Go files. `go test -race` passes on their packages,
  and so does `make race`. `check_records.py --check-all` exits 0.
* After P2, the guard's `pre-push`, run read-only, passes the real outgoing commits. A scratch
  clone with one planted string for each rule is refused once per rule.
* After D4, the issue body no longer matches the zone or the alias.

## Pros and Cons of the Options

### A — Every class, then the rules

* Good, because it does what the owner asked, by 0171-MADR's tested method.
* Good, because the order of D3 means no push is ever blocked by surprise.
* Bad, because one commit touches code, tests, fixtures, guides and records, so it needs the full
  gates and a careful read of the diff.

### B — The zone only

* Good, because it is smaller.
* Bad, because the relay's address and the host aliases stay published, against the owner's
  instruction.

### C — A, plus a history rewrite

* Good, because the originals leave the published history.
* Bad, because it means force-pushing `master`, which breaks every clone and every cited hash.
  The zone and the relay's address stay public through DNS anyway.

## More Information

* The plan: [0232-PLAN-redact-real-zones-hostnames-and-ips-and-deny-them.md](0232-PLAN-redact-real-zones-hostnames-and-ips-and-deny-them.md).
* The precedent: [0171-MADR-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md](0171-MADR-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md).
* The guard and its list belong to the owner's dotfiles. This pair appends four lines to the list,
  with the owner's go-ahead, and does not change the tool.
