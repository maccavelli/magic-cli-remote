# Grok agent pre-add / pre-commit gates

These hooks mirror the Claude Code setup in `.claude/settings.json` so Grok
runs the same staging gates:

1. **pre-add-go.sh** — `gofmt` / `golint` / `govulncheck` before `git add` of Go
2. **pre-add-dart.sh** — `dart format --set-exit-if-changed` before `git add` of Dart
3. **pre-commit-msg.sh** — block `git commit -m` / `--message` / `-F` (hook message)

Scripts live under `.claude/hooks/` (single implementation). This JSON only
registers them for Grok’s `PreToolUse` matcher on `Bash` / `run_terminal_command`.

Project must be trusted (`/hooks-trust` or `--trust`). This repo is listed in
`~/.grok/trusted_folders.toml`.

Git’s own `pre-commit` (`scripts/pre-commit.sh` via `make install-hooks`) remains
the backstop: staged-file checks + `go test -race ./...` on every commit.
