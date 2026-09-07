#!/usr/bin/env bash
# Extract the failing_test field for a MADR 0143 ledger row.
#
# Prefer the first Go test failure line matching ^--- FAIL: (\S+). When the
# log has no such line — a compile failure, vet-style abort inside go test,
# or infrastructure noise — fall back to the failing step name so the field
# is never empty (Phase 2 exit criterion).
#
# Invoked from nick-fields/retry's on_retry_command, which runs after attempt 1
# fails and before attempt 2 starts. The attempt log must therefore be the
# tee'd output of attempt 1 (see ci.yml Test step).
#
# Usage:
#   scripts/ci-flake-capture.sh --log PATH --out PATH --step NAME
#
# Exit codes: 0 always writes --out (even on fallback); 2 bad arguments.
set -euo pipefail

log=""; out=""; step=""

die() { printf 'ci-flake-capture: %s\n' "$1" >&2; exit 2; }

while [ "$#" -gt 0 ]; do
  case "$1" in
  --log)  [ "$#" -ge 2 ] || die "--log needs a path"; log="$2"; shift 2 ;;
  --out)  [ "$#" -ge 2 ] || die "--out needs a path"; out="$2"; shift 2 ;;
  --step) [ "$#" -ge 2 ] || die "--step needs a name"; step="$2"; shift 2 ;;
  --*)    die "unknown flag: $1" ;;
  *)      die "unexpected operand: $1" ;;
  esac
done

[ -n "$log" ]  || die "need --log"
[ -n "$out" ]  || die "need --out"
[ -n "$step" ] || die "need --step"

mkdir -p "$(dirname -- "$out")"

failing=""
if [ -f "$log" ]; then
  # First match only: a multi-test failure still records one name; the ledger
  # is per-retry, not per-assertion. \S+ stops at the space before (duration).
  failing="$(awk '/^--- FAIL: / { print $3; exit }' "$log" || true)"
fi

if [ -z "$failing" ]; then
  failing="$step"
fi

# Refuse empty: a blank field would break the 7-column contract and the
# awk verifier in the plan.
[ -n "$failing" ] || die "refusing to write an empty failing_test"

printf '%s\n' "$failing" > "$out"
printf 'ci-flake-capture: failing_test=%s\n' "$failing"
