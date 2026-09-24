---
status: proposed
date: 2026-09-24
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Redact the identifiers already in the tree, so pushes pass the disclosure guard

## Context and Problem Statement

Since 2026-09-23 a global pre-push hook guards every push to github.com. It is the owner's
dotfiles MADR 0010, `github-disclosure.py`. It refuses a push when any outgoing commit carries
an entry of the owner's identifier deny list, whether in a file, a path, a message, an
identity, a tag or a ref name.

On 2026-09-24 it refused this repository's first push since then: the 8 commits of MADRs 0169
and 0170. Every hit is in content these commits did not write. They are identifiers published
long before the guard existed. Until the tree is clean, no commit can be pushed, however clean
its own diff.

### What was measured, not assumed

- **What the guard scans.** It scans every blob of the **full tree** of each outgoing commit,
  not the diff (`tree_locations` in `github-disclosure.py`). Its own help says a fix committed
  on top is not enough: every outgoing commit has to be rewritten.
- **The refusal.** 556 hits (60 listed, "and 496 more"). The guard, run read-only with the ref
  line git would pass, prints remedy steps only for kinds it found. Those steps are "redact
  file content" and "squash", with no message, identity, path, tag or ref remedy. So every hit
  is file content.
- **`github-disclosure.py redact`** (dry run) would edit **61 tracked files**, and rename none:

  | Area | Files | What the identifier is |
  | --- | --- | --- |
  | `docs/spec/` records | 37 | a user name in quoted paths, or a host name in prose |
  | `docs/kilo-spike-7.4.20/` captures | 15 | a user name inside captured paths (8 of them single-line JSON) |
  | `docs/0100-findings-update-refresh.md` | 1 | a host name in quoted commands |
  | Go tests | 3 | a relay host ID (`internal/cli/pair_test.go`, `internal/config/config_test.go`); a directory path in two captured kilo frames (`internal/provider/kilo/dialect_test.go`) |
  | Dart tests | 3 | the relay host ID in `connect_screen_test.dart`, `settings_screen_test.dart`, `relay_inner_tls_test.dart` |
  | wire fixtures | 2 | the user name in each `meta.json`'s `redacted` note (`kilo/testdata/wire/7.5.6`, `opencode/testdata/wire/1.18.26`) |

- **The tool's placeholders are not valid test values.** It would turn a relay host ID into
  `<mac-host>`, including inside a pairing URL's `&hid=` query parameter. The tests would
  assert on a value no real host could have, and could exercise different parsing.
- **Nothing reads the captures at run time.** The seven code references to
  `docs/kilo-spike-7.4.20/` are comments. The captures are evidence, like prose.
- **Two defects in the tool itself**, both measured on Windows:
  - `redact` crashes with `UnicodeEncodeError` (cp1252) when its output is redirected and a
    diff line holds a non-ASCII character (here U+2264). `PYTHONIOENCODING=utf-8` works
    around it.
  - Files without a trailing newline print their `+` line glued to the next file's header, so
    a parser of its diff miscounts them. This affected 8 files here; the tool's own count, 61,
    is right.

### Findings

- **F1 — The guard makes every earlier leak a blocker for every future push.** This is by
  design: it is how it guarantees nothing is published. The 61 files must be clean before
  anything else can go out.
- **F2 — A redaction on top of the unpushed commits is not enough.** Each of the 8 commits
  carries the old tree. They must be rebuilt on a clean base, or squashed.
- **F3 — The 61 files are two kinds.** Prose and evidence (53 files: records, captures, the
  findings doc) can take the tool's placeholders as they stand. Test inputs and fixtures
  (8 files) need neutral values that keep the test's meaning.
- **F4 — Only content is affected.** No commit message, identity, path, tag or ref name is
  flagged. The rebuilt commits' new messages must stay that way, and are checked.
- **F5 — The redaction diff itself contains the identifiers**, on its removed lines. So the
  commit-message generator, which reads the staged diff, could echo one into a message. The
  guard checks messages too, so this is caught before any push.

## Decision Drivers

- The owner's guard is a standing instruction for everything published from these machines.
  It is never bypassed.
- Tests must keep testing the same behaviour after redaction.
- The per-phase commits of 0169 and 0170 are the plan's record of what was done. Keep them
  separate if that can be done safely.
- Nothing is lost if a step goes wrong: every rewrite starts from a backup ref.

## Considered Options

- **A — One redaction commit on `origin/master`, then rebuild the unpushed commits on it**, one
  by one, each with a fresh hook-generated message.
- **B — Redact, then squash** all unpushed work into one commit, as the guard's message
  suggests.
- **C — Hold every push** until the owner changes the guard or its deny list.
- **D — Apply the tool's placeholders everywhere**, test values included.

## Decision Outcome

Chosen option: **A**, the owner's choice on 2026-09-24. It keeps the history's meaning while
publishing nothing the guard forbids.

### The decisions

- **D1 — Redact the whole tree once, in one commit on top of `origin/master`.**
  - The 53 prose and evidence files get the tool's placeholders: `redact --apply`, with UTF-8
    output.
  - The 8 test and fixture files get neutral valid values instead: the relay host ID becomes
    `mac-host`, and the captured directory becomes `/Users/user/…`. The two `meta.json` notes
    take the tool's placeholder, because they are prose.
  - This commit contains nothing else.
- **D2 — Rebuild, don't squash.** Each unpushed commit is replayed onto the redaction commit in
  order, with `git cherry-pick --no-commit` then `git commit --no-edit`, so each gets a new
  message from the hook.
  - A replay that conflicts stops the rebuild; nothing is resolved by guesswork.
  - The old `master` is kept as a backup branch until the push has landed.
- **D3 — The rebuilt tree must equal the old tree plus the redaction, exactly.** `git diff` from
  the backup to the rebuilt tip may touch only the 61 files, and only the redacted lines.
- **D4 — Nothing is pushed until the guard, run read-only, reports zero hits** against the
  rebuilt branch, messages included. A message that echoes an identifier (F5) is regenerated
  by re-committing through the hook, never edited by hand.
- **D5 — The tool's two defects are reported to the owner**, whose dotfiles repository owns
  the tool. They are not fixed from here.

### Consequences

- Good, because the push unblocks, and CI can finally run 0170's new gates.
- Good, because each commit of 0169 and 0170 survives as its own commit, with an honest message.
- Bad, because every rebuilt commit gets a new hash. The records that cite hashes (`919204b`,
  `8944b53`, `bd03cab`, `1c45fe5`, `9f15fbb`, `ff3b604`, `038abf9`, `7e601eb`, `0768315`) need
  those citations updated in the same rebuild.
- Neutral, because the old identifiers stay in `origin`'s existing history. This record stops
  new publication. It does not rewrite what is already public, which is the owner's call alone.

### Confirmation

```sh
PYTHONIOENCODING=utf-8 python ~/.global-git-hooks/github-disclosure.py redact   # after D1: "would edit 0 file(s)"
git diff --stat backup/pre-0171..master                                          # the 61 files only (D3)
go test ./internal/cli ./internal/config ./internal/provider/kilo                 # the redacted Go tests pass
flutter test test/connect_screen_test.dart test/settings_screen_test.dart test/relay_inner_tls_test.dart
# guard, read-only, on the rebuilt branch: 0 hits (D4); then git push; then a dispatched ci.yml run is green
```

## Pros and Cons of the Options

### A — One redaction commit, then rebuild the unpushed commits on it (chosen)

- Good, because it keeps each phase's commit, and the tree equality check (D3) proves nothing
  else changed.
- Bad, because the rebuild is 12 replays that must each apply cleanly (the 11 unpushed commits
  counted on 2026-09-24, plus this record's), and every hash cited in the records changes.

### B — Redact, then squash

- Good, because it is two commands.
- Bad, because a day of per-phase commits becomes one, and the records' commit citations point
  at nothing.

### C — Hold every push

- Good, because it changes nothing.
- Bad, because this repository could not publish anything, CI included, for as long as the
  guard and the tree disagree.

### D — The tool's placeholders everywhere

- Good, because it is one command.
- Bad, because the tests would assert `<mac-host>` host IDs and URL query values no real host
  produces, which changes what they test.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| The guard scans full trees of every outgoing commit | `~/.global-git-hooks/github-disclosure.py`, `tree_locations` and `pre_push` |
| 556 hits, all file content | the guard's `pre-push` run read-only with the ref line for `master`, 2026-09-24 |
| 61 files, by area | `github-disclosure.py redact` (dry run), diff summarized per file |
| The test values are relay host IDs and one captured directory | the dry-run hunks for the 6 test files |
| Captures are not read at run time | whole-repository search: 7 code references, all comments |
| The tool's Windows defects | its traceback (cp1252) and its diff output for files with no trailing newline |

### Related records

- [0170-MADR](0170-MADR-make-preflight-true-hermetic-install-tests-and-pinned-staticcheck.md):
  its CI acceptance (A4) waits for this push.
- The owner's dotfiles MADR 0010, which defines the guard. It lives outside this repository.
