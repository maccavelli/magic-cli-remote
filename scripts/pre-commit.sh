#!/usr/bin/env bash
# Git pre-commit hook: the Go pre-add checks over the staged files, then tests.
#
# Backstop for the rule in AGENTS.md — the agent hook
# (.claude/hooks/pre-add-go.sh) gates `git add`, and this catches anything staged
# another way. Both call scripts/go-precheck.sh so the checks cannot drift.
#
# Install with `make install-hooks`, which also makes sure this repo's hooks are
# reachable: a global core.hooksPath makes Git ignore .git/hooks entirely.
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT" || exit 1

# Staged Go files (added/copied/modified — deletions have nothing to check).
mapfile -t STAGED_GO < <(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)
mapfile -t STAGED_DART < <(git diff --cached --name-only --diff-filter=ACM | grep '\.dart$' || true)

echo "============================================="
echo "   Pre-Commit Checks"
echo "============================================="

if [ "${#STAGED_GO[@]}" -gt 0 ]; then
  echo "--> gofmt / golint / govulncheck (${#STAGED_GO[@]} staged Go file(s))..."
  if ! ./scripts/go-precheck.sh "${STAGED_GO[@]}"; then
    echo
    echo "ERROR: staged Go files did not pass the pre-add checks." >&2
    echo "Fix them (start with 'make fmt'), 'git add' again, and re-commit." >&2
    exit 1
  fi
else
  echo "--> no staged Go files; skipping Go checks."
fi

# CI runs `dart format --set-exit-if-changed` over apps/mobile, so an unformatted
# Dart file is a red build even when analyze and the tests pass locally — which is
# exactly how one slipped through. Check the staged files only (fast), and only
# when the toolchain is here: a Go-only contributor should not be blocked.
if [ "${#STAGED_DART[@]}" -gt 0 ]; then
  if command -v dart >/dev/null 2>&1; then
    echo "--> dart format (${#STAGED_DART[@]} staged Dart file(s))..."
    if ! dart format --output=none --set-exit-if-changed "${STAGED_DART[@]}"; then
      echo
      echo "ERROR: staged Dart files are not formatted." >&2
      echo "Run 'dart format ${STAGED_DART[*]}', 'git add' again, and re-commit." >&2
      exit 1
    fi
  else
    echo "--> dart not installed; skipping Dart format check."
  fi
fi

echo "--> Running Go test suite (with race detector)..."
if ! go test -race ./...; then
  echo
  echo "ERROR: tests failed." >&2
  exit 1
fi

echo ""
echo "============================================="
echo "   All checks passed!"
echo "============================================="
exit 0
