#!/usr/bin/env bash
# Fail when the local Flutter SDK does not match the version CI pins.
#
# MADR 0127 D7 gate 2. CI cannot observe a developer's machine, and a host
# running ahead of the pin is a configuration this repository has already been
# burned by twice:
#
#   * 0112 finding 4 — a lockfile produced by a newer SDK than CI pinned;
#   * 0124 :70-76    — a host on 3.47.1 against a pinned 3.44.8, whose weaker
#                      analyzer missed lints CI enforces and cost a red run.
#
# The pin in .github/workflows/ci.yml is the single source of truth; this reads
# it rather than duplicating the number.
#
# Env:
#   MC_CI_FILE  workflow file to read the pin from (default: the repo's ci.yml)
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
ci_file="${MC_CI_FILE:-$root/.github/workflows/ci.yml}"

if [ ! -f "$ci_file" ]; then
  echo "assert-flutter-pin: workflow file not found: $ci_file" >&2
  exit 2
fi

pinned="$(sed -n 's/^[[:space:]]*FLUTTER_VERSION:[[:space:]]*"\{0,1\}\([0-9][0-9.]*\)"\{0,1\}[[:space:]]*$/\1/p' "$ci_file" | head -1)"
if [ -z "$pinned" ]; then
  echo "assert-flutter-pin: no FLUTTER_VERSION found in $ci_file" >&2
  exit 2
fi

if ! command -v flutter >/dev/null 2>&1; then
  echo "assert-flutter-pin: skipped (flutter not installed)"
  exit 0
fi

# `flutter --version` line 1: "Flutter 3.47.2 • channel stable • <url>"
local_ver="$(flutter --version 2>/dev/null | sed -n 's/^Flutter \([0-9][0-9.]*\).*/\1/p' | head -1)"
if [ -z "$local_ver" ]; then
  echo "assert-flutter-pin: could not parse 'flutter --version' output" >&2
  exit 2
fi

if [ "$local_ver" != "$pinned" ]; then
  echo "assert-flutter-pin: FAIL" >&2
  echo "  local  Flutter $local_ver" >&2
  echo "  pinned Flutter $pinned   ($ci_file)" >&2
  echo >&2
  echo "A host that differs from the pin resolves pubspec.lock differently and" >&2
  echo "analyzes with a different lint set (MADR 0127 D7). Reconcile with:" >&2
  if [ "$(printf '%s\n%s\n' "$local_ver" "$pinned" | sort -V | head -1)" = "$local_ver" ]; then
    echo "    flutter upgrade                 # local is older than the pin" >&2
  else
    echo "    flutter downgrade $pinned       # local is newer than the pin" >&2
  fi
  echo "  or move the pin deliberately, in the same commit as the lockfile." >&2
  exit 1
fi

echo "assert-flutter-pin: OK local and pinned Flutter agree ($pinned)"
