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
