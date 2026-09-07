#!/usr/bin/env bash
# on_retry_command entry point for the MADR 0143 flake ledger.
#
# Exists because of a cross-platform defect found by the Phase 2 probe (run
# 34044623679). The capture call used to be written inline in ci.yml as a
# multi-line command with backslash continuations. nick-fields/retry does not
# run on_retry_command through the step's `shell: bash` on Windows, so the
# continuations were not honoured: the trailing "\" arrived as a literal
# argument and ci-flake-capture.sh exited 2 with "unexpected operand: \".
# The Windows row then fell back to the step name instead of naming the test.
#
# The fix is shape, not logic. This script takes NO arguments and reads
# everything from the environment, so the workflow invocation is a single bare
# token — nothing for an outer shell to split, quote, continue or expand. It
# behaves identically whether the caller is bash, cmd or PowerShell.
#
# Environment:
#   RUNNER_TEMP  required; the job-scoped temp dir the attempt log lives under
#   FLAKE_STEP   optional; step-name fallback for failing_test (default: Test)
set -euo pipefail

die() { printf 'ci-flake-on-retry: %s\n' "$1" >&2; exit 2; }

# Takes no arguments, and says so loudly. A stray operand means the caller
# split the command — the exact Windows failure this wrapper exists to
# prevent — and that must be a visible error, not a silent ignore.
[ "$#" -eq 0 ] || die "takes no arguments; got $#: $*"

tmp="${RUNNER_TEMP:-}"
[ -n "$tmp" ] || die "RUNNER_TEMP is not set"

step="${FLAKE_STEP:-Test}"
dir="$tmp/ci-flake"

here="$(cd "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

exec bash "$here/ci-flake-capture.sh" \
  --log "$dir/attempt.log" \
  --out "$dir/failing-test.txt" \
  --step "$step"
