#!/usr/bin/env bash
# Go pre-add checks: format, lint, vulnerabilities.
#
# The single implementation of the rule in AGENTS.md, called from three places so
# they cannot drift: `make pre-add-check`, the git pre-commit hook
# (scripts/pre-commit.sh), and the agent hook that gates `git add`
# (.claude/hooks/pre-add-go.sh).
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
  golint) echo "  install: go install golang.org/x/lint/golint@latest" >&2 ;;
  govulncheck) echo "  install: go install golang.org/x/vuln/cmd/govulncheck@latest" >&2 ;;
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
  echo "$unformatted" | sed 's/^/  /' >&2
  failed=1
fi

# 2. golint, per file so the output names what to fix.
if need golint; then
  lint_out=""
  for f in "${files[@]}"; do
    out="$(golint "$f" 2>&1)"
    [ -n "$out" ] && lint_out+="$out"$'\n'
  done
  if [ -n "$lint_out" ]; then
    echo "golint:" >&2
    printf '%s' "$lint_out" | sed 's/^/  /' >&2
    failed=1
  fi
else
  failed=2
fi

# 3. govulncheck, over the module. It reports *called* vulnerabilities, so it is
# a property of the whole build rather than of the edited files. ~7s here.
if [ "${GO_PRECHECK_SKIP_VULN:-0}" = "1" ]; then
  echo "govulncheck: skipped (GO_PRECHECK_SKIP_VULN=1)" >&2
elif need govulncheck; then
  vuln_out="$(govulncheck ./... 2>&1)"
  vuln_rc=$?
  if [ "$vuln_rc" -ne 0 ]; then
    # A vulnerability database that cannot be reached is not a finding. Warn and
    # continue, rather than making offline work impossible — the finding case
    # below is what must block.
    if printf '%s' "$vuln_out" | grep -qiE 'no such host|connection refused|timeout|dial tcp|proxy'; then
      echo "govulncheck: could not reach the vulnerability database; skipped." >&2
    else
      echo "govulncheck:" >&2
      printf '%s' "$vuln_out" | tail -30 | sed 's/^/  /' >&2
      failed=1
    fi
  elif ! printf '%s' "$vuln_out" | grep -q 'No vulnerabilities found'; then
    echo "govulncheck:" >&2
    printf '%s' "$vuln_out" | tail -30 | sed 's/^/  /' >&2
    failed=1
  fi
else
  failed=2
fi

if [ "$failed" -eq 0 ]; then
  echo "go-precheck: ${#files[@]} file(s) clean (gofmt, golint, govulncheck)."
fi
exit "$failed"
