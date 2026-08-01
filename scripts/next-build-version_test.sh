#!/usr/bin/env bash
# Self-check for next-build-version.sh (runs in a throwaway git repo).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="$root/scripts/next-build-version.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

git -C "$tmp" init -q
git -C "$tmp" config user.email "test@example.com"
git -C "$tmp" config user.name "test"
echo x >"$tmp/f"
git -C "$tmp" add f
git -C "$tmp" commit -qm init
git -C "$tmp" tag v0.2.1

# Copy script into isolation via env pointing at real script but git -C via...
# Script uses its own root from dirname. Run with a wrapper by placing a copy.
mkdir -p "$tmp/scripts"
cp "$script" "$tmp/scripts/next-build-version.sh"
chmod +x "$tmp/scripts/next-build-version.sh"

export MCREMOTE_VERSION_PUSH=0
export MCREMOTE_VERSION_TAG=1
export BUILD_COUNTER_FILE="$tmp/.build-counter"

# Simulate being in that repo by running script from there
cd "$tmp"

v1="$(./scripts/next-build-version.sh)"
v2="$(./scripts/next-build-version.sh)"

echo "v1=$v1 v2=$v2"

# With no push, versions get a .gXXXX uniqueness suffix after BASE.N
[[ "$v1" == 0.2.1.1* ]] || { echo "expected 0.2.1.1*, got $v1"; exit 1; }
[[ "$v2" == 0.2.1.2* ]] || { echo "expected 0.2.1.2*, got $v2"; exit 1; }
[[ "$v1" != "$v2" ]] || { echo "versions should differ"; exit 1; }

# Tags should exist for build/0.2.1.1 and build/0.2.1.2 (local)
git tag -l 'build/*'

# With tagging disabled, pure counter
export MCREMOTE_VERSION_TAG=0
export BUILD_COUNTER_FILE="$tmp/.build-counter-2"
v3="$(./scripts/next-build-version.sh 1.0.0)"
v4="$(./scripts/next-build-version.sh 1.0.0)"
[[ "$v3" == "1.0.0.1" ]] || { echo "got $v3"; exit 1; }
[[ "$v4" == "1.0.0.2" ]] || { echo "got $v4"; exit 1; }

# --- push default (MADR 0060 D2) -------------------------------------------
# Everything above pins MCREMOTE_VERSION_PUSH, so the DEFAULT path was never
# exercised. It must be: push in CI, never from a local build — otherwise a
# developer's `make install` appends build/* tags to the public remote.
origin="$tmp/origin.git"
git init -q --bare "$origin"
git -C "$tmp" remote add origin "$origin"
git -C "$tmp" push -q origin HEAD:refs/heads/main

unset MCREMOTE_VERSION_PUSH
export MCREMOTE_VERSION_TAG=1
export BUILD_COUNTER_FILE="$tmp/.build-counter-3"

# Local build (no CI in the environment): must not push.
env -u CI -u GITHUB_ACTIONS ./scripts/next-build-version.sh 2.0.0 >/dev/null
pushed="$(git -C "$origin" tag -l 'build/*' | wc -l | tr -d ' ')"
[[ "$pushed" == "0" ]] || {
	echo "local build pushed $pushed build/* tag(s) to origin; expected 0"
	git -C "$origin" tag -l 'build/*'
	exit 1
}

# CI build: must still push, so the remote stays the shared ledger.
CI=true ./scripts/next-build-version.sh 3.0.0 >/dev/null
pushed_ci="$(git -C "$origin" tag -l 'build/3.0.0.*' | wc -l | tr -d ' ')"
[[ "$pushed_ci" == "1" ]] || {
	echo "CI build pushed $pushed_ci build/3.0.0.* tag(s); expected 1"
	exit 1
}

echo "OK"
