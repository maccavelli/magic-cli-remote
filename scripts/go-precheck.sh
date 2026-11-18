#!/usr/bin/env bash
# Go pre-add checks: format, lint, vulnerabilities.
#
# The single implementation of the rule in AGENTS.md, called from two places so
# they cannot drift: `make pre-add-check`, and the machine-wide agent gate that
# blocks `git add` (~/.global-agent-hooks/pre-add-go.sh), which prefers this
# script whenever the repository being staged into ships one.
#
# Usage:
#   scripts/go-precheck.sh [file.go ...]
#
# With no arguments it checks every tracked Go file; with arguments, only those
# (non-Go arguments are ignored, so callers can pass a whole changed-file list).
#
# Exit codes: 0 all clear · 1 a check failed · 2 a required tool is missing.
#
# Env:
#   GO_PRECHECK_SKIP_VULN=1   skip govulncheck (offline work; the pre-commit
#                             hook still runs it)
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT" || exit 1

# Collect the Go files to check.
files=()
if [ "$#" -gt 0 ]; then
  for f in "$@"; do
    case "$f" in
    *.go) [ -f "$f" ] && files+=("$f") ;;
    esac
  done
  # Arguments that contain no Go files are not an error: a mixed `git add` of
  # docs and Dart has nothing for this script to say.
  [ "${#files[@]}" -eq 0 ] && exit 0
else
  while IFS= read -r f; do
    [ -n "$f" ] && files+=("$f")
  done < <(git ls-files '*.go')
fi

need() {
  command -v "$1" >/dev/null 2>&1 && return 0
  echo "go-precheck: $1 not found in PATH." >&2
  case "$1" in
  python3) echo "  python3 runs scripts/lint.py, which runs the pinned golangci-lint." >&2 ;;
  esac
  return 1
}

failed=0

# 1. gofmt. This repo is formatted with plain gofmt, not gofumpt: gofumpt
# reflows unrelated code (var blocks, call arguments), which turns every commit
# into a diff full of noise.
need gofmt || exit 2
unformatted="$(gofmt -l "${files[@]}")"
if [ -n "$unformatted" ]; then
  echo "gofmt: these files are not formatted (run 'make fmt' or 'gofmt -w <file>'):" >&2
  printf '%s\n' "$unformatted" | sed 's/^/  /' >&2
  failed=1
fi

# 2. golangci-lint (MADR 0131), through scripts/lint.py, the implementation `make lint` and CI
# also run, so this gate cannot pass what CI would refuse. golangci-lint analyses packages, not
# files: with arguments it runs over the packages that hold them (every build GOOS CI covers, and
# every opt-in tag, as `make lint` does; a few seconds a package), with none over `./...`.
need python3 || exit 2
lint_dirs=()
if [ "$#" -gt 0 ]; then
  while IFS= read -r d; do
    [ -n "$d" ] && lint_dirs+=("$d")
  done < <(for f in "${files[@]}"; do dirname "$f"; done | sort -u)
fi
lint_out="$(python3 "$REPO_ROOT/scripts/lint.py" "${lint_dirs[@]+"${lint_dirs[@]}"}" 2>&1)"
case $? in
0) ;;
2)
  printf '%s\n' "$lint_out" | sed 's/^/  /' >&2
  failed=2
  ;;
*)
  echo "golangci-lint:" >&2
  printf '%s\n' "$lint_out" | sed 's/^/  /' >&2
  [ "$failed" -eq 0 ] && failed=1
  ;;
esac

# 3. govulncheck, over the module: scripts/vulncheck.sh fails on a called, imported or
# required-module finding, apart from its allowlist (MADR 0174). It is a property of the
# whole build rather than of the edited files. ~7s here.
if [ "${GO_PRECHECK_SKIP_VULN:-0}" = "1" ]; then
  echo "govulncheck: skipped (GO_PRECHECK_SKIP_VULN=1)" >&2
else
  # Its findings go to stderr; its "clear" line is left out of this summary.
  "$REPO_ROOT/scripts/vulncheck.sh" >/dev/null
  case $? in
  0) ;;
  2) failed=2 ;;
  *) [ "$failed" -eq 0 ] && failed=1 ;;
  esac
fi

if [ "$failed" -eq 0 ]; then
  echo "go-precheck: ${#files[@]} file(s) clean (gofmt, golangci-lint, govulncheck)."
fi
exit "$failed"
