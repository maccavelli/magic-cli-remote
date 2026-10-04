# AGENTS.md

## Task tracking

**Use the todowrite tool for every task that spans multiple steps.** Write todos before starting work and mark them `completed` immediately after each item is done — not when the phase is finished.

- Write todos before the first tool call of a new task.
- Update the status in real time; never batch completions.
- Keep exactly one `in_progress` item at a time.
- Mark `completed` only after verification (build, test, lint).
- When the user says "proceed to the next phase", close out the previous phase's todos first, then write the new phase's list.

## Sandbox escalation

When in-scope work needs a path outside the workspace's granted filesystem roots, request a narrowly scoped sandbox escalation immediately. State the command's purpose and the affected path. Do not stop at a sandbox denial when escalation can complete the requested work. Report the exact path and error if escalation is declined or still fails.

## The pre-add rule (Go)

**No Go file is staged until `gofmt`, `golint` and `govulncheck` are clean.** `git add` is where the tree becomes what will be committed.

```bash
make pre-add-check                    # every tracked Go file
make pre-add-check FILES="a.go b.go"  # just these
./scripts/go-precheck.sh a.go b.go    # same thing, directly
```

`scripts/go-precheck.sh` is the only implementation. `make pre-add-check` and the per-machine agent gates (`~/.global-agent-hooks/`) all call it. There is no git `pre-commit` hook; a `git add` typed in a plain terminal is ungated — `make pre-add-check` is the manual equivalent.

- **gofmt** — plain `gofmt`, not `gofumpt`. `make fmt` formats `cmd` and `internal`.
- **golint** — per file so the output names what to fix. Any output fails.
- **govulncheck** — whole module (~7s). A vulnerability fails; an unreachable vuln DB only warns. `GO_PRECHECK_SKIP_VULN=1` skips one run.

**Dart:** CI runs `dart format --output=none --set-exit-if-changed .` over `apps/mobile`. Format each staged `.dart` file. `make preflight` runs the full mobile trio (`flutter analyze`, `flutter test`, `dart format`).

Silence from a hook is indistinguishable from success. Probe from this repo:

```bash
printf 'package main\nfunc  X( ){\n}\n' > ztest.go
echo '{"tool_input":{"command":"git add ztest.go"}}' | ~/.global-agent-hooks/pre-add-go.sh; echo "exit=$?"   # want 2
rm ztest.go
```

Agents load hook config at session start. There is nothing to bypass: fix the file.

## Tests

**Windows local gates (0145):** on a Windows host, before push run `make ci-windows`; before tag also `make ci-windows-smoke`; functional paths/pair/doctor → `scripts/acceptance-windows.ps1`. On macOS/Linux those targets skip and exit 0 — use `make preflight`. See `docs/guides/ops-windows-install.md` and MADR/PLAN 0145. No workflow edits without Mac permission.

**Run `make` on Windows from Git Bash, or from PowerShell with `C:\Program Files\Git\usr\bin` on `PATH`.** GNU make needs `sh.exe`. Check: `make -n ci-windows` must show `[ "windows" != "windows" ]`. In PowerShell, `bash` is WSL, not Git Bash.

`make test`, and `make race` / `go test -race ./...` before a commit. Live-tagged tests spend real tokens; run them at acceptance: `make live-grok`, `make live-opencode`, `make live-kilo`, `make live-codex`.

## Commit messages

**Do not pass a commit message (`-m`, `-M`, `--message`, or `-F`).** A global `prepare-commit-msg` hook fills `.git/COMMIT_EDITMSG`. Run `git commit --no-edit`. A bare `git commit` opens vim and hangs a headless agent. Do not write the subject or body, and do not use `GIT_EDITOR=true`. Grok's always-on copy lives in `~/.grok/rules/git-prepare-commit-msg.md`.

## Web fetching

After a failed `webfetch` tool result, immediately use `curl`. Do not retry `webfetch`.

## MADR and PLAN before mutating work

Rationale: [docs/decisions/0105-MADR-mutating-work-requires-madr-and-plan.md](docs/decisions/0105-MADR-mutating-work-requires-madr-and-plan.md). Per-agent pointers: `.claude/rules/madr-and-plan-skill.md`, `.grok/rules/madr-plan-before-mutating-work.md`, `.opencode/rules.md`.

**This file is the normative copy of the gate.** Those pointers carry the skill name and the gate, and point here. Do not restate this section in them.

Whenever the user asks for an MADR and a plan, load **`madr-and-plan-writing`** first and follow it. The name is the filesystem `name:` field. Verify:

```bash
ls -d ~/.claude/skills/*madr* && grep '^name:' ~/.claude/skills/*madr*/SKILL.md
```

The command outranks the prose. If they disagree, the filesystem is right — fix this section.

**Read-only investigation needs no pair.** Mutating work does. Before the first write, name the `docs/decisions/NNNN-MADR-*` / `docs/decisions/NNNN-PLAN-*` pair being executed, or stop and write one.

Mutating: create, edit, or delete files; stage or commit (except the bootstrap exception); dependency or lockfile changes; CI / config / hook changes; builds or installers that write the tree, `$HOME`, or a live service; generating committed artifacts.

Order: (1) investigate (2) write or amend the MADR (`status: proposed` unless already decided); present; do not implement (3) write or amend the PLAN; present (4) mutate only after explicit approval (`proceed`, `execute the plan`, `do phase N`); stay inside that PLAN (5) out-of-scope discoveries wait: amend, re-approve, continue.

Same topic: amend that number. Greenfield: next unused `NNNN`, new pair, same slug.

Bootstrap exception: authoring `docs/decisions/NNNN-MADR-*`, `docs/decisions/NNNN-PLAN-*`, this section, and the per-agent process rules under `.claude/rules/`, `.grok/rules/` and `.opencode/rules.md` does not require a *prior* pair. Putting source, tests, CI, or product config in that same commit is a violation.

`git push` and tags still need an explicit ask in the same turn.

Record format, naming, numbering, and how to record a deviation: `madr-and-plan-writing`. Docs-tree layout: `documentation-writing`.

### Host identifiers never appear in records

This repository is public. Nothing committed carries a hostname, account name, or absolute path with a real user in it. Use placeholders (`C:\Users\<user>\...`, `<HOST>\<group>`, `<owner-account>`). Redact as you write, including when quoting live evidence.

## File naming

Numbered records live in `docs/decisions/` (MADR, PLAN) and `docs/reports/` (REPORT, GATES); guides are unnumbered in `docs/guides/`. Flutter-companion records live under `apps/mobile/docs/` (same `NNNN` sequence). Filenames carry the kind infix and a zero-padded 4-digit number (`NNNN-MADR-*` / `NNNN-PLAN-*` / `NNNN-REPORT-*` / `NNNN-GATES-*`), unique across the whole repository; a MADR and its PLAN share the number. The next number comes from `scripts/check_records.py --next`; `make check-records` validates links and pairing. Cite records by full filename, never by number alone. When a decision rests on how an external CLI behaves, record the probe evidence in the MADR and pin it with a live-tagged test. Full naming and numbering rules: `madr-and-plan-writing`. Docs-tree layout: `documentation-writing`.
