# AGENTS.md

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
| agent pre-add hooks | blocks an agent's `git add`/`git stage` when the Go files it would stage do not pass: Claude and Grok load `.claude/settings.json`; Codex uses `.agents/hooks.json`; OpenCode loads `.opencode/plugins/pre-add-go-gate.ts`; Goose loads `.agents/plugins/pre-add-go-gate/` |
| `.git/hooks/pre-commit` (from `scripts/pre-commit.sh`, installed by `make install-hooks`) | backstop over the staged files, then `go test -race ./...` |
| `make pre-add-check` | manual / CI invocation |

### Dart, too

`flutter analyze` and `flutter test` passing is **not** enough: CI also runs
`dart format --output=none --set-exit-if-changed .` over `apps/mobile`, so one
unformatted file is a red build with green tests. The pre-commit hook checks
`dart format` over staged `.dart` files for that reason (skipped when `dart` is
not installed). `make preflight` runs the full mobile trio.

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
  possible. `GO_PRECHECK_SKIP_VULN=1` skips it for one run; the pre-commit hook
  still runs it.

### Setup, and the trap that hides it

```bash
make install-hooks   # installs the hook and proves Git can reach it
make verify-hooks    # just the check
```

This machine sets a global `core.hooksPath`, and **Git then looks only there** —
`.git/hooks` is ignored completely, so a hook installed by `make install-hooks`
never runs and nothing reports an error. (Five commits landed that way before it
was noticed.) `install-hooks` now detects this and drops
`scripts/git-hooks-chain.sh` into the configured hooks directory, which delegates
back to the repository's own hook. The alternative, if you would rather not touch
the global directory:

```bash
git config --local core.hooksPath .git/hooks
```

`make verify-hooks` resolves the path Git will actually use and fails if nothing
executable answers for `pre-commit`. Run it if you ever wonder whether the checks
are really running — silence from a hook is indistinguishable from success.

### Bypassing

Not as a habit. When it is genuinely necessary (a WIP commit on a scratch
branch), `git commit --no-verify` skips the git hook and the reason belongs in the
commit message. The agent hook has no bypass: fix the file.

## Tests

`make test`, and `make race` / `go test -race ./...` before a commit — the
pre-commit hook runs the race suite for you. Live-tagged tests need the real
CLIs: `go test -tags live_grok ./...`, `-tags live_opencode ./...`. They spend
real tokens; run them at acceptance, not in a loop.

## Docs

Architectural decisions go in `docs/00NN-*.md` (MADR). When a decision rests on
how an external CLI behaves, record the probe evidence in the MADR and pin it
with a live-tagged test: CLI behaviour changes silently, and an assumption with
no test is a future bug report.
