---
status: proposed
date: 2026-11-01
decision-makers: Project Owner
consulted: none
informed: every agent working in this repository
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Allow read-only agent commands and tools without a prompt

## Context and Problem Statement

Agents working in this repository already hit a prompt for every shell
invocation that is not on a short allow-list. That list lives in two
places and covers only a handful of *write-adjacent* build commands:

* [`.claude/settings.json`](../../.claude/settings.json) — Claude Code
  `permissions.allow`, currently `go build|test|vet|…`, `gofmt -l`,
  `git check-ignore`, a few `make`/`mingw32-make` targets, and
  `./bin/mcremote` / `./bin/mcrelay` `--help`/`version`/`paths`.
* [`opencode.json`](../../opencode.json) — OpenCode `permission.bash`,
  the same patterns, plus Windows `.\bin\…` spellings. The default for
  unmatched bash is `"ask"`.

Neither file allows the tools agents use on every turn (`Read`, `Grep`,
`Glob`, directory listing) nor the read-only shell that investigation
needs (`git status`, `git log`, `git diff`, `ls`, `rg`,
`python3 scripts/check_records.py --next`, `make -n …`). Codex, Grok,
and Kilo have no in-repo permission file at all. Gobble had none either:
its project rules live in `.gobble/settings.yaml` (`permissions.rules`),
not in Claude/OpenCode JSON.

The owner asked for an allow-list of **commands and tools** for this
repo that covers **all sane read-only** operations, so investigation
does not stall on a prompt while mutating work still does.

* Implementation Plan: [0227-PLAN-agent-readonly-command-and-tool-allowlist.md](0227-PLAN-agent-readonly-command-and-tool-allowlist.md)

## Decision Drivers

* Investigation (MADR 0105's read-only phase) should not require a
  human click per `git status` or file read.
* Mutating shell (`git add`, `git commit`, `rm`, `mv`, `make` targets
  that write, `go generate`, network clients) must still prompt unless
  already allow-listed as a *build* command the owner has accepted.
* One policy, three encodings: Claude Code, OpenCode, and gobble's
  `.gobble/settings.yaml`. Do not invent a fourth schema. Codex / Grok /
  Kilo stay unchanged unless those harnesses grow an equivalent in-repo
  file.
* Patterns must not be so wide that `Bash(*)` or `Bash(git *)`
  silently covers `git push` or `git commit`.
* Host identifiers never appear in the committed lists.

## Considered Options

* Keep the current build-only lists and keep prompting for reads
* Expand the existing Claude and OpenCode files, and add gobble's
  `.gobble/settings.yaml`, with explicit read-only command patterns
  and read-only tool names; leave mutate as ask
* Allow all Bash and all tools (`Bash(*)`, default allow)

## Decision Outcome

Chosen option: "Expand the existing Claude and OpenCode files, and add
gobble's `.gobble/settings.yaml`, with explicit read-only command
patterns and read-only tool names; leave mutate as ask", because it
matches the owner's request, uses each harness's own file, and does
not punch a hole large enough for `git push`.

### Consequences

* Good, because `git status`, `git diff`, `ls`, `rg`, record-number
  lookup, and the harness Read/Grep/Glob tools stop prompting.
* Good, because the existing `go test` / `make test` allows stay;
  this decision does not revoke them.
* Neutral, because Codex, Grok, and Kilo still have no in-repo
  permission file; their product defaults apply. Gobble's defaults
  already run read-only built-ins without a rule; the YAML still names
  them so bash and the build commands match Claude/OpenCode.
* Bad, because an explicit list will miss a read-only command until
  someone adds it; that is the cost of not using `Bash(*)`.
* Bad, because `go test` and `make test` write caches under the
  module; they remain allowed as they are today, not because they are
  read-only.

### Confirmation

* After the PLAN lands, a read-only turn (`git status`, `git log -1`,
  `python3 scripts/check_records.py --next`, Read of `AGENTS.md`)
  completes with no permission prompt on Claude Code, OpenCode, and
  gobble (once the project is trusted).
* A mutating command not on the list (`git add`, `rm`, `curl`) still
  prompts.
* `make check-records` still pairs this MADR with its PLAN.

## Pros and Cons of the Options

### Keep the current build-only lists

* Good, because no extra surface.
* Bad, because investigation still prompts on every `git status`.

### Expand the existing Claude and OpenCode files (chosen)

* Good, because the policy is committed and reviewable.
* Good, because patterns stay named, not `*`.
* Bad, because three files must stay in sync by hand.

### Allow all Bash and all tools

* Good, because nothing is missed.
* Bad, because `git push` and `rm -rf` would auto-run.

## More Information

Read-only **tools** (Claude Code names; OpenCode maps these to its
built-in tools and does not need them in `permission.bash`):

* `Read`, `Grep`, `Glob`, `LS` / directory listing, `Task` (read
  investigation), `TodoRead` if present.

Not auto-allowed: `Write`, `Edit`, `NotebookEdit`, `Bash` as a tool
(only the named `Bash(…)` patterns), `WebFetch`, `WebSearch`, MCP
tools that mutate or leave the host.

Read-only **commands** to add (prefix match, same style as today):

* git: `status`, `log`, `diff`, `show`, `rev-parse`, `branch` (no
  `-d`/`-D`/`-m` forms as separate allows — those stay ask),
  `ls-files`, `describe`, `check-ignore` (already), `remote -v`,
  `tag --list` / `tag -l`, `stash list`, `config --get`,
  `rev-list`, `name-rev`, `merge-base`, `shortlog`, `blame`,
  `cat-file`, `ls-tree`, `symbolic-ref`.
* inspection: `ls`, `pwd`, `head`, `tail`, `wc`, `file`, `stat`,
  `uname`, `date`, `which`, `type`, `command -v`, `rg`, `grep`,
  `find`, `tree` (the binary, not `rm -rf`), `diff`, `cmp`, `md5`
  / `shasum` / `sha256sum`.
* Go / Make inspection already partly present; add `go fmt -l` is
  already `gofmt -l`; add `make -n *`, `make help`, `make pre-add-check`
  (read linters; it does not stage).
* records: `python3 scripts/check_records.py --next` and
  `python3 scripts/check_records.py --help`.
* Flutter/Dart inspection: `flutter --version`, `dart --version`,
  `flutter devices`, `flutter doctor -v` (network-ish; still
  read-only of the machine). Prefer ask for `flutter pub get`.
* binaries already allowed for `--help` / `version` / `paths` stay.

Still **ask** (not exhaustive): `git add`, `git commit`, `git push`,
`git checkout`, `git switch`, `git stash push`, `rm`, `mv`, `cp`,
`mkdir`, `chmod`, `curl`, `wget`, `ssh`, `scp`, `docker`,
`kubectl`, `gh`, `npm publish`, `go get` that changes `go.mod`.

Existing `go test *` / `make test` / `go build *` remain allowed;
this MADR does not reclassify them as read-only.

## Amendment — 2026-11-01: gobble project settings

The owner required this repository's own gobble permission file.
Gobble does not read `.claude/settings.json`. Project rules are
`.gobble/settings.yaml` under `permissions.rules` (`allow` / `ask` /
`deny` of `<tool>` or `<tool>(<pattern>)`). Tool names are gobble's:
`read`, `grep`, `find`, `tree`, `todo`, `bash`. Built-in read-only
tools already run with no rule; the file still `allow`s them and the
same bash prefixes as the other two files. `allowBypass` stays false.
The project file loads only after trust (`gobble trust` or equivalent).
Host paths never appear in the YAML.
