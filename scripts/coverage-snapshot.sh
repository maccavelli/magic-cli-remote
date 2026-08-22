#!/usr/bin/env bash
# Capture default-tag unit-coverage profiles for MADR 0112 A13.
#
# Writes one Go cover profile per package plus an optional Flutter LCOV file
# into OUTPUT_DIR. Profile names are the package path with "/" replaced by "_",
# so a directory is reproducible and diffable across runs.
#
# Usage:
#   scripts/coverage-snapshot.sh --output OUTPUT_DIR --go GO_PACKAGE [GO_PACKAGE ...] [--flutter apps/mobile]
#
# Live-tagged, token-bearing and loopback tests are deliberately excluded: the
# coverage floors in MADR 0112 A13 are unit floors and must not be satisfiable
# by a probe that needs a real agent binary.
#
# Exit codes: 0 all profiles captured · 1 a test run or argument was bad.
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)" || exit 1
cd "$REPO_ROOT" || exit 1

output=""
flutter_root=""
go_pkgs=()
mode=""

die() {
  printf 'coverage-snapshot: %s\n' "$1" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
  --output)
    [ "$#" -ge 2 ] || die "--output needs a directory"
    output="$2"
    mode=""
    shift 2
    ;;
  --flutter)
    [ "$#" -ge 2 ] || die "--flutter needs a root"
    flutter_root="$2"
    mode=""
    shift 2
    ;;
  --go)
    mode="go"
    shift
    ;;
  --*)
    die "unknown flag: $1"
    ;;
  *)
    [ "$mode" = "go" ] || die "unexpected operand: $1"
    go_pkgs+=("$1")
    shift
    ;;
  esac
done

[ -n "$output" ] || die "--output is required"
[ "${#go_pkgs[@]}" -gt 0 ] || die "--go needs at least one package"

mkdir -p "$output" || die "cannot create $output"

for pkg in "${go_pkgs[@]}"; do
  name="${pkg#./}"
  name="${name//\//_}"
  profile="$output/$name.cover"
  if ! go test -count=1 -covermode=atomic -coverprofile="$profile" "$pkg" >"$output/$name.log" 2>&1; then
    printf 'coverage-snapshot: go test failed for %s\n' "$pkg" >&2
    tail -n 40 "$output/$name.log" >&2
    exit 1
  fi
  [ -s "$profile" ] || die "empty profile for $pkg"
done

if [ -n "$flutter_root" ]; then
  lcov="$output/flutter.lcov"
  if ! (cd "$flutter_root" && flutter test --coverage --coverage-path="$lcov") >"$output/flutter.log" 2>&1; then
    printf 'coverage-snapshot: flutter test failed\n' >&2
    tail -n 40 "$output/flutter.log" >&2
    exit 1
  fi
  [ -s "$lcov" ] || die "empty LCOV at $lcov"
fi

printf 'coverage-snapshot: wrote %d Go profile(s)%s to %s\n' \
  "${#go_pkgs[@]}" "$([ -n "$flutter_root" ] && printf ' and flutter.lcov')" "$output"
