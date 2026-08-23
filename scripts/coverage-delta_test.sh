#!/usr/bin/env bash
# Fixture-driven tests for scripts/coverage-delta.sh (MADR 0112 A13, PLAN P0).
#
# Every case uses the deterministic profiles under scripts/testdata/coverage/,
# never a real test run, so the tool's arithmetic is provable without depending
# on the repository's own coverage. Run with: bash scripts/coverage-delta_test.sh
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)" || exit 1
cd "$REPO_ROOT" || exit 1

DELTA="scripts/coverage-delta.sh"
FIX="scripts/testdata/coverage"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/coverage-delta-test.XXXXXX")" || exit 1
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0

# stage DIR [go:PKG=FIXTURE ...] [lcov=FIXTURE]
stage() {
  local dir="$1"; shift
  mkdir -p "$dir"
  local spec
  for spec in "$@"; do
    case "$spec" in
    go:*)
      local body="${spec#go:}"
      local pkg="${body%%=*}"
      local fixture="${body#*=}"
      local name="${pkg#./}"
      cp "$FIX/$fixture" "$dir/${name//\//_}.cover"
      ;;
    lcov=*)
      cp "$FIX/${spec#lcov=}" "$dir/flutter.lcov"
      ;;
    *)
      printf 'stage: bad spec %s\n' "$spec" >&2
      exit 2
      ;;
    esac
  done
}

# expect NAME WANT_EXIT -- COMMAND...
expect() {
  local name="$1" want="$2"
  shift 3 # name, want, "--"
  local out rc
  out="$("$@" 2>&1)"
  rc=$?
  if [ "$rc" -eq "$want" ]; then
    pass=$((pass + 1))
    printf 'ok   %s\n' "$name"
  else
    fail=$((fail + 1))
    printf 'FAIL %s (exit %d, want %d)\n%s\n' "$name" "$rc" "$want" "$out"
  fi
}

# expect_json NAME PATTERN -- COMMAND...
expect_json() {
  local name="$1" pattern="$2"
  shift 3
  local out
  out="$("$@" 2>&1)"
  if printf '%s' "$out" | grep -q "$pattern"; then
    pass=$((pass + 1))
    printf 'ok   %s\n' "$name"
  else
    fail=$((fail + 1))
    printf 'FAIL %s (missing %s)\n%s\n' "$name" "$pattern" "$out"
  fi
}

GP="./internal/gate"
OC="./internal/provider/opencode"

# --- absolute floor: exact boundary behaviour -------------------------------
stage "$WORK/f80" "go:$GP=go_exactly80.cover"
expect "floor accepts exactly 80.0%" 0 -- \
  "$DELTA" floor --after "$WORK/f80" --minimum 80.0 --go "$GP"

stage "$WORK/f7999" "go:$GP=go_just_under80.cover"
expect "floor rejects 79.999%" 1 -- \
  "$DELTA" floor --after "$WORK/f7999" --minimum 80.0 --go "$GP"

stage "$WORK/f80large" "go:$GP=go_exactly80_large.cover"
expect "floor accepts exactly 80.0% at scale" 0 -- \
  "$DELTA" floor --after "$WORK/f80large" --minimum 80.0 --go "$GP"

stage "$WORK/f82" "go:$GP=go_exactly82.cover"
expect "floor accepts the P0A 82.0% target" 0 -- \
  "$DELTA" floor --after "$WORK/f82" --minimum 82.0 --go "$GP"

expect "floor rejects 80.0% against the 82.0% target" 1 -- \
  "$DELTA" floor --after "$WORK/f80" --minimum 82.0 --go "$GP"

# --- the OpenCode-specific floor -------------------------------------------
stage "$WORK/oc82" "go:$OC=go_exactly82.cover"
expect "opencode floor rejects 82% when 85% is required" 1 -- \
  "$DELTA" floor --after "$WORK/oc82" --minimum 80.0 --opencode-floor 85.0 --go "$OC"

stage "$WORK/oc85" "go:$OC=go_exactly85.cover"
expect "opencode floor accepts exactly 85.0%" 0 -- \
  "$DELTA" floor --after "$WORK/oc85" --minimum 80.0 --opencode-floor 85.0 --go "$OC"

expect "the general floor still applies to other packages" 1 -- \
  "$DELTA" floor --after "$WORK/f7999" --minimum 80.0 --opencode-floor 85.0 --go "$GP"

# --- degenerate inputs ------------------------------------------------------
stage "$WORK/zero" "go:$GP=go_zero.cover"
expect "floor rejects a 0% package" 1 -- \
  "$DELTA" floor --after "$WORK/zero" --minimum 80.0 --go "$GP"

stage "$WORK/nostmt" "go:$GP=go_no_statements.cover"
expect "a package with no executable statements is neutral" 0 -- \
  "$DELTA" floor --after "$WORK/nostmt" --minimum 80.0 --go "$GP"

stage "$WORK/badmode" "go:$GP=go_missing_mode.cover"
expect "a profile with no mode header is rejected" 1 -- \
  "$DELTA" floor --after "$WORK/badmode" --minimum 80.0 --go "$GP"

stage "$WORK/badcount" "go:$GP=go_bad_count.cover"
expect "a profile with a non-numeric count is rejected" 1 -- \
  "$DELTA" floor --after "$WORK/badcount" --minimum 80.0 --go "$GP"

expect "a missing profile is rejected" 1 -- \
  "$DELTA" floor --after "$WORK/does-not-exist" --minimum 80.0 --go "$GP"

# --- phase: non-regression and strict improvement ---------------------------
expect "phase fails when nothing improved" 1 -- \
  "$DELTA" phase --before "$WORK/f80" --after "$WORK/f80" --minimum 80.0 --go "$GP"
expect_json "phase names the no-improvement reason" 'no executable target strictly improved' -- \
  "$DELTA" phase --before "$WORK/f80" --after "$WORK/f80" --minimum 80.0 --go "$GP"

stage "$WORK/imp" "go:$GP=go_improved.cover"
expect "phase passes on a strict improvement" 0 -- \
  "$DELTA" phase --before "$WORK/f80" --after "$WORK/imp" --minimum 80.0 --go "$GP"

expect "phase rejects a fraction regression" 1 -- \
  "$DELTA" phase --before "$WORK/imp" --after "$WORK/f80" --minimum 80.0 --go "$GP"
expect_json "phase names the regressed fraction" 'fraction_regressed' -- \
  "$DELTA" phase --before "$WORK/imp" --after "$WORK/f80" --minimum 80.0 --go "$GP"
# 90/100 -> 80/100 violates the fraction rule and the uncovered rule at once;
# the report must name both, not only the last one evaluated.
expect_json "a fraction regression names its exact counts" '90/100 -> 80/100' -- \
  "$DELTA" phase --before "$WORK/imp" --after "$WORK/f80" --minimum 80.0 --go "$GP"

# 80/100 -> 80000/100000 keeps the exact fraction but multiplies uncovered by
# 1000. The count alone is not a failure — a phase that adds well-covered
# surface always raises it — so this fails only because nothing improved.
expect "an equal fraction with more uncovered statements is not itself a failure" 1 -- \
  "$DELTA" phase --before "$WORK/f80" --after "$WORK/f80large" --minimum 80.0 --go "$GP"
expect_json "the failure is the missing improvement, not the uncovered count" 'no executable target strictly improved' -- \
  "$DELTA" phase --before "$WORK/f80" --after "$WORK/f80large" --minimum 80.0 --go "$GP"
expect_json "the uncovered rise is still reported, marked informational" 'informational' -- \
  "$DELTA" phase --before "$WORK/f80" --after "$WORK/f80large" --minimum 80.0 --go "$GP"

# The real case the rule change is for: new surface raises the uncovered count
# while improving the fraction. That must pass.
stage "$WORK/newsurface_before" "go:$GP=go_exactly80.cover"
stage "$WORK/newsurface_after" "go:$GP=go_exactly82.cover"
expect "adding surface that improves the fraction passes despite more uncovered" 0 -- \
  "$DELTA" phase --before "$WORK/f80" --after "$WORK/newsurface_after" --minimum 80.0 --go "$GP"

# A phase that improves but lands below the floor still fails.
stage "$WORK/lowbefore" "go:$GP=go_zero.cover"
expect "phase enforces the floor even on an improvement" 1 -- \
  "$DELTA" phase --before "$WORK/lowbefore" --after "$WORK/f7999" --minimum 80.0 --go "$GP"

# --- Dart: aggregate, per-file, and new-file floors -------------------------
stage "$WORK/d80" "go:$GP=go_exactly80.cover" "lcov=lcov_exactly80.info"
expect "flutter aggregate accepts exactly 80.0%" 0 -- \
  "$DELTA" floor --after "$WORK/d80" --minimum 80.0 --go "$GP" --dart-root apps/mobile

stage "$WORK/d7999" "go:$GP=go_exactly80.cover" "lcov=lcov_just_under80.info"
expect "flutter aggregate rejects 79.999%" 1 -- \
  "$DELTA" floor --after "$WORK/d7999" --minimum 80.0 --go "$GP" --dart-root apps/mobile

stage "$WORK/dfile" "go:$GP=go_exactly80.cover" "lcov=lcov_file_below_floor.info"
expect "a sub-floor Dart file fails even when the aggregate passes" 1 -- \
  "$DELTA" floor --after "$WORK/dfile" --minimum 80.0 --go "$GP" \
  --dart-root apps/mobile --dart-file lib/low.dart
expect "the same capture passes for a compliant file" 0 -- \
  "$DELTA" floor --after "$WORK/dfile" --minimum 80.0 --go "$GP" \
  --dart-root apps/mobile --dart-file lib/a.dart

expect "a requested Dart file absent from LCOV is an error" 1 -- \
  "$DELTA" floor --after "$WORK/d80" --minimum 80.0 --go "$GP" \
  --dart-root apps/mobile --dart-file lib/not_present.dart
expect_json "the absent Dart file is named" 'not_in_lcov' -- \
  "$DELTA" floor --after "$WORK/d80" --minimum 80.0 --go "$GP" \
  --dart-root apps/mobile --dart-file lib/not_present.dart

stage "$WORK/dnew90" "go:$GP=go_exactly80.cover" "lcov=lcov_new_dart_90.info"
expect "a new Dart file at exactly 90.0% passes" 0 -- \
  "$DELTA" floor --after "$WORK/dnew90" --minimum 80.0 --go "$GP" \
  --dart-root apps/mobile --new-dart-file lib/features/new_sheet.dart

stage "$WORK/dnew899" "go:$GP=go_exactly80.cover" "lcov=lcov_new_dart_899.info"
expect "a new Dart file at 89.9% fails the 90.0% floor" 1 -- \
  "$DELTA" floor --after "$WORK/dnew899" --minimum 80.0 --go "$GP" \
  --dart-root apps/mobile --new-dart-file lib/features/new_sheet.dart
expect "the same 89.9% file would pass the 80.0% existing-file floor" 0 -- \
  "$DELTA" floor --after "$WORK/dnew899" --minimum 80.0 --go "$GP" \
  --dart-root apps/mobile --dart-file lib/features/new_sheet.dart

# lib/data/models.dart is a suffix-trap for lib/data/protocol/models.dart.
stage "$WORK/dsuffix" "go:$GP=go_exactly82.cover" "lcov=lcov_suffix_trap.info"
expect "a path that is a suffix of another resolves exactly" 1 -- \
  "$DELTA" floor --after "$WORK/dsuffix" --minimum 82.0 --go "$GP" \
  --dart-root apps/mobile --dart-file lib/data/protocol/models.dart
expect "and its longer sibling resolves to its own counts" 0 -- \
  "$DELTA" floor --after "$WORK/dsuffix" --minimum 82.0 --go "$GP" \
  --dart-root apps/mobile --dart-file lib/data/models.dart

# --- baseline: comparison against a committed summary -----------------------
cat > "$WORK/baseline.json" <<'JSON'
{
  "targets": [
    {"kind": "go", "target": "./internal/gate", "covered": 80, "total": 100},
    {"kind": "dart", "target": "<application>", "covered": 80, "total": 100}
  ]
}
JSON
expect "baseline passes when the fresh capture matches" 0 -- \
  "$DELTA" baseline --baseline-json "$WORK/baseline.json" --after "$WORK/d80" \
  --minimum 80.0 --opencode-floor 85.0 --dart-root apps/mobile
expect "baseline passes on an improvement" 0 -- \
  "$DELTA" baseline --baseline-json "$WORK/baseline.json" --after "$WORK/imp" \
  --minimum 80.0 --opencode-floor 85.0 --go "$GP"
stage "$WORK/regressed" "go:$GP=go_zero.cover"
expect "baseline rejects a regression below the committed counts" 1 -- \
  "$DELTA" baseline --baseline-json "$WORK/baseline.json" --after "$WORK/regressed" \
  --minimum 80.0 --opencode-floor 85.0 --go "$GP"

# --- argument handling ------------------------------------------------------
expect "a gate with no requested target is a usage error" 2 -- \
  "$DELTA" floor --after "$WORK/f80" --minimum 80.0
expect "baseline with no requested target is a usage error" 2 -- \
  "$DELTA" baseline --baseline-json "$WORK/baseline.json" --after "$WORK/f80" --minimum 80.0
expect "an unknown subcommand is a usage error" 2 -- "$DELTA" bogus --after "$WORK/f80"
expect "an unknown flag is a usage error" 2 -- "$DELTA" floor --after "$WORK/f80" --nope
expect "phase without --before is a usage error" 2 -- "$DELTA" phase --after "$WORK/f80" --go "$GP"
expect "a malformed --minimum is a usage error" 2 -- \
  "$DELTA" floor --after "$WORK/f80" --minimum eighty --go "$GP"

# --- the tool never writes the repository ----------------------------------
before_state="$(git status --porcelain)"
"$DELTA" floor --after "$WORK/f80" --minimum 80.0 --go "$GP" >/dev/null 2>&1
if [ "$before_state" = "$(git status --porcelain)" ]; then
  pass=$((pass + 1)); printf 'ok   the tool leaves the working tree unchanged\n'
else
  fail=$((fail + 1)); printf 'FAIL the tool modified the working tree\n'
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
