#!/bin/bash
# Git pre-commit hook to verify all tests pass before committing.
set -euo pipefail

# Find repository root
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

echo "============================================="
echo "   Running Pre-Commit Verification Tests     "
echo "============================================="

echo "--> Running Go test suite (with race detector)..."
go test -race ./...

echo ""
echo "--> Running Flutter unit and widget tests..."
cd apps/mobile
flutter test

echo ""
echo "============================================="
echo "   ✨ All tests passed! Committing files.    "
echo "============================================="
exit 0
