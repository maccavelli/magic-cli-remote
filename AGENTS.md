# AGENTS.md

## Task tracking

**Use the todowrite tool for every task that spans multiple steps.** Write todos
before starting work and mark them `completed` immediately after each item is
done — not when the phase is finished. A completed commit without updated todos
hides progress from the user and makes it impossible to resume cleanly after
interruption.

Checklist:

- Write todos before the first tool call of a new task.
- Update the status in real time; never batch completions.
- Keep exactly one `in_progress` item at a time.
- Mark `completed` only after verification (build, test, lint).
- When the user says "proceed to the next phase", close out the previous phase's
  todos first, then write the new phase's list.

## Sandbox escalation

When work that is in scope needs to read from or write to a path outside the
workspace's granted filesystem roots, request a narrowly scoped sandbox
escalation immediately. State the command's purpose and the affected path in
the approval request. Do not report the repository filesystem as read-only or
stop at a sandbox denial when escalation can safely complete the requested
work. Report the exact path and error if escalation is declined or still fails.

## The pre-add rule (Go)

**No Go file is staged until `gofmt`, `golint` and `govulncheck` are clean.**
Fix the code first: `git add` is the point where the tree becomes what will be
committed, so that is where correctness is checked — not after the fact.

Run it yourself with either:

```bash
make pre-add-check                    # every tracked Go file
make pre-add-check FILES="a.go b.go"  # just these
./scripts/go-precheck.sh a.go b.go    # same thing, directly
```

`scripts/go-precheck.sh` is the only implementation of the rule. Everything else
calls it, so the checks cannot drift apart:

| enforcement point | what it covers |
|---|---|
| agent pre-add gates | block an agent's `git add`/`git stage` when the checks fail. Configured **per machine, not per repo** — see below |
| `make pre-add-check` | manual / CI invocation |

There is deliberately **no git `pre-commit` hook**. The gate belongs on
`git add`, where the tree becomes what will be committed; a second copy of the
same checks at commit time was only ever a backstop for the case where an agent
staged files without its hooks running, and it cost a full `go test -race` on
every commit. If you want that safety net, run `make race` yourself.

### Where the gates actually live

Not in this repository. They are installed once per machine and apply to every
checkout, so a repo does not have to carry six agents' worth of hook config to
be protected:

```
~/.global-agent-hooks/          # the scripts; see its README.md
```

Registered there for claude, grok, goose, opencode, kilo and agy. Each agent's
gate runs `scripts/go-precheck.sh` **from the repository being staged into**
when that repo has one, so this project's checks stay this project's checks —
elsewhere the gate falls back to plain `gofmt`.

The same directory installs post-edit formatters (`gofmt` / `dart format` on
write), which is why files usually arrive at `git add` already clean.

A consequence worth knowing: a `git add` typed in a plain terminal is not
gated by anything. That is the trade for having no commit-time hook — `make
pre-add-check` is the manual equivalent.

### Dart, too

`flutter analyze` and `flutter test` passing is **not** enough: CI also runs
`dart format --output=none --set-exit-if-changed .` over `apps/mobile`, so one
unformatted file is a red build with green tests. The agent gate checks
`dart format` over the `.dart` files being staged for that reason (skipped when
`dart` is not installed). `make preflight` runs the full mobile trio.

Editing Dart through a tool that does not format on write? Run `dart format` on
the file before staging it.

What each check means here:

- **gofmt** — plain `gofmt`, *not* `gofumpt`. gofumpt reflows unrelated code (var
  blocks, multi-line call arguments), which buries a real change in noise.
  `make fmt` formats `cmd` and `internal`.
- **golint** — run per file so the output names what to fix. Any output fails.
- **govulncheck** — over the whole module, since it reports *called*
  vulnerabilities rather than per-file ones (~7s). A vulnerability fails; a
  vulnerability database that cannot be reached only warns, so offline work stays
  possible. `GO_PRECHECK_SKIP_VULN=1` skips it for one run.

### Checking the gate is really running

Silence from a hook is indistinguishable from success, so test it rather than
assume — from anywhere in this repo:

```bash
printf 'package main\nfunc  X( ){\n}\n' > ztest.go
echo '{"tool_input":{"command":"git add ztest.go"}}' | ~/.global-agent-hooks/pre-add-go.sh; echo "exit=$?"   # want 2
rm ztest.go
```

Agents load hook config at session start, so a change to it needs a new session.

### Bypassing

There is nothing to bypass at commit time. The agent gate has no bypass either:
fix the file.

## Tests

`make test`, and `make race` / `go test -race ./...` before a commit — nothing
runs the race suite for you, so run it. Live-tagged tests need the real
CLIs: `go test -tags live_grok ./...`, `-tags live_opencode ./...`. They spend
real tokens; run them at acceptance, not in a loop.

## Commit messages (Git hook auto-generation)

**Do NOT pass a commit message (`-m`, `-M`, `--message`, or `-F`) when executing `git commit`.**

A global `prepare-commit-msg` git hook automatically generates and populates commit messages.

- Run `git commit` without `-m` or `--message`.
- This rule applies across all agent environments: Antigravity CLI (`agy`), Claude, Codex, OpenCode, Grok, and Goose.
- The rule is stated in `~/AGENTS.md` for every agent on this machine; it is a
  rule, not a gate, so honour it rather than expecting a hook to catch it.

## Web fetching

After a failed `webfetch` tool result, immediately use `curl` instead — do not
retry `webfetch`. This applies to web fetches for documentation, APIs, or any
other URL-based content.

## File naming: MADR and plan files

All files in `docs/` must use a zero-padded 4-digit number as a prefix. This
keeps them grouped consistently in directory listings and makes it easy to
distinguish MADR files from plan files at a glance.

- **MADR files** follow the pattern `NNNN-MADR-name-of-file.md`. For example:
  `0022-MADR-name-of-file.md`.
- **Plan files** follow the pattern `NNNN-PLAN-name-of-file.md`. For example:
  `0023-PLAN-name-of-file.md`.

The number prefix must be unique and sequential. A MADR and its accompanying
plan share the same number — `NNNN-MADR-*` and `NNNN-PLAN-*` refer to the
same topic. When a decision rests on how an
external CLI behaves, record the probe evidence in the MADR and pin it with a
live-tagged test: CLI behaviour changes silently, and an assumption with no test
is a future bug report.
