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

echo "OK"
