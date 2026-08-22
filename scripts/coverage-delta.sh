#!/usr/bin/env bash
# Enforce the MADR 0112 A13 unit-coverage contract from captured profiles.
#
#   scripts/coverage-delta.sh phase    --before BEFORE_DIR --after AFTER_DIR --minimum 80.0 \
#                                      --go GO_PACKAGE [GO_PACKAGE ...] \
#                                      [--dart-root apps/mobile --dart-file DART_FILE ...]
#   scripts/coverage-delta.sh floor    --after AFTER_DIR --minimum 80.0 --opencode-floor 85.0 \
#                                      --go GO_PACKAGE [GO_PACKAGE ...] \
#                                      [--dart-root apps/mobile --dart-file DART_FILE ...] \
#                                      [--new-dart-file DART_FILE ...]
#   scripts/coverage-delta.sh baseline --baseline-json SUMMARY_JSON --after AFTER_DIR \
#                                      --minimum 80.0 --opencode-floor 85.0 \
#                                      [--dart-root apps/mobile --dart-file DART_FILE ...]
#
# All comparisons use exact integer counts. Percentages are never parsed from
# console output and never rounded before a comparison: a threshold of 80.0 is
# applied as covered*1000 >= total*800, so 79.999% fails and exactly 80.0%
# passes. Every subcommand prints a stable JSON summary on stdout and writes
# nothing into the repository.
#
# "phase" additionally enforces the per-phase rules: no target's exact fraction
# may fall, no target's uncovered count may rise, and at least one executable
# target must strictly improve. "floor" checks absolute thresholds only and is
# therefore portable across CI operating systems. "baseline" compares a fresh
# capture with the committed sanitized summary on the same platform.
#
# Exit codes: 0 all rules satisfied · 1 a rule failed · 2 bad arguments/input.
set -uo pipefail

sub="${1:-}"
[ "$#" -gt 0 ] && shift

before=""; after=""; baseline_json=""
minimum="80.0"; opencode_floor=""; new_dart_floor="90.0"
dart_root=""
go_pkgs=(); dart_files=(); new_dart_files=()
mode=""

die() { printf 'coverage-delta: %s\n' "$1" >&2; exit 2; }

while [ "$#" -gt 0 ]; do
  case "$1" in
  --before)          [ "$#" -ge 2 ] || die "--before needs a directory"; before="$2"; mode=""; shift 2 ;;
  --after)           [ "$#" -ge 2 ] || die "--after needs a directory"; after="$2"; mode=""; shift 2 ;;
  --baseline-json)   [ "$#" -ge 2 ] || die "--baseline-json needs a file"; baseline_json="$2"; mode=""; shift 2 ;;
  --minimum)         [ "$#" -ge 2 ] || die "--minimum needs a value"; minimum="$2"; mode=""; shift 2 ;;
  --opencode-floor)  [ "$#" -ge 2 ] || die "--opencode-floor needs a value"; opencode_floor="$2"; mode=""; shift 2 ;;
  --new-dart-floor)  [ "$#" -ge 2 ] || die "--new-dart-floor needs a value"; new_dart_floor="$2"; mode=""; shift 2 ;;
  --dart-root)       [ "$#" -ge 2 ] || die "--dart-root needs a path"; dart_root="$2"; mode=""; shift 2 ;;
  --go)              mode="go"; shift ;;
  --dart-file)       mode="dart"; shift ;;
  --new-dart-file)   mode="newdart"; shift ;;
  --*)               die "unknown flag: $1" ;;
  *)
    case "$mode" in
    go)      go_pkgs+=("$1") ;;
    dart)    dart_files+=("$1") ;;
    newdart) new_dart_files+=("$1") ;;
    *)       die "unexpected operand: $1" ;;
    esac
    shift
    ;;
  esac
done

case "$sub" in
phase)
  [ -n "$before" ] || die "phase needs --before"
  [ -n "$after" ] || die "phase needs --after"
  ;;
floor)
  [ -n "$after" ] || die "floor needs --after"
  ;;
baseline)
  [ -n "$after" ] || die "baseline needs --after"
  [ -n "$baseline_json" ] || die "baseline needs --baseline-json"
  ;;
*)
  die "usage: coverage-delta.sh {phase|floor|baseline} ..."
  ;;
esac

[ -n "$opencode_floor" ] || opencode_floor="$minimum"

# A gate that inspects nothing must not report success. Every subcommand needs
# at least one executable target, otherwise a phase could "pass" by forgetting
# to pass --go.
if [ "${#go_pkgs[@]}" -eq 0 ] && [ -z "$dart_root" ] \
  && [ "${#dart_files[@]}" -eq 0 ] && [ "${#new_dart_files[@]}" -eq 0 ]; then
  die "no targets requested: pass --go, --dart-root, --dart-file or --new-dart-file"
fi

# permille converts "80.0" to the exact integer 800 so every threshold test is
# integer arithmetic. Two decimals are accepted and scaled to per-ten-thousand
# only when needed; one decimal is the documented grammar.
permille() {
  printf '%s' "$1" | awk '
    {
      n = $0
      if (n !~ /^[0-9]+(\.[0-9]+)?$/) { exit 2 }
      split(n, p, ".")
      whole = p[1] + 0
      frac = (length(p) > 1) ? p[2] : "0"
      while (length(frac) < 1) frac = frac "0"
      frac = substr(frac, 1, 1)
      printf "%d", whole * 10 + frac
    }'
}

min_pm="$(permille "$minimum")" || die "bad --minimum: $minimum"
oc_pm="$(permille "$opencode_floor")" || die "bad --opencode-floor: $opencode_floor"
nd_pm="$(permille "$new_dart_floor")" || die "bad --new-dart-floor: $new_dart_floor"

profile_name() { local p="${1#./}"; printf '%s' "${p//\//_}"; }

# go_counts prints "covered total" for one cover profile, counting each block's
# statement count once. A malformed line is a hard error rather than a silent
# zero: a truncated profile must never read as full coverage.
go_counts() {
  local f="$1"
  [ -f "$f" ] || { printf 'coverage-delta: missing profile %s\n' "$f" >&2; return 2; }
  awk '
    NR == 1 { if ($0 !~ /^mode: /) { print "BADMODE" > "/dev/stderr"; exit 3 } ; next }
    /^[[:space:]]*$/ { next }
    {
      if (NF < 3) { print "BADLINE" > "/dev/stderr"; exit 3 }
      stmts = $(NF-1); count = $NF
      if (stmts !~ /^[0-9]+$/ || count !~ /^[0-9]+$/) { print "BADNUM" > "/dev/stderr"; exit 3 }
      total += stmts
      if (count > 0) covered += stmts
    }
    END { printf "%d %d", covered + 0, total + 0 }
  ' "$f"
}

# lcov_counts prints "covered total" for one LCOV source path suffix, or for the
# whole file when the suffix is the literal "*". An absent requested file is an
# error: silently skipping it would let a phase drop a target from its gate.
lcov_counts() {
  local f="$1" want="$2"
  [ -f "$f" ] || { printf 'coverage-delta: missing lcov %s\n' "$f" >&2; return 2; }
  awk -v want="$want" '
    /^SF:/ { sf = substr($0, 4); take = (want == "*") ? 1 : (index(sf, want) > 0 && length(sf) - index(sf, want) + 1 == length(want)) ; next }
    /^LF:/ { if (take) total += substr($0, 4) + 0; next }
    /^LH:/ { if (take) covered += substr($0, 4) + 0; next }
    END {
      if (!seen && total == 0 && want != "*") { }
      printf "%d %d", covered + 0, total + 0
    }
  ' "$f"
}

fail=0
improved=0
json_rows=""

emit_row() {
  local kind="$1" name="$2" cov="$3" tot="$4" status="$5" detail="$6"
  local pct="0.0000"
  [ "$tot" -gt 0 ] && pct="$(awk -v c="$cov" -v t="$tot" 'BEGIN{printf "%.4f", 100*c/t}')"
  [ -n "$json_rows" ] && json_rows="$json_rows,"
  json_rows="$json_rows
    {\"kind\":\"$kind\",\"target\":\"$name\",\"covered\":$cov,\"total\":$tot,\"uncovered\":$((tot-cov)),\"percent\":$pct,\"status\":\"$status\",\"detail\":\"$detail\"}"
}

# check_floor applies an exact integer threshold.
check_floor() {
  local cov="$1" tot="$2" pm="$3"
  [ "$tot" -eq 0 ] && return 0
  [ $((cov * 1000)) -ge $((tot * pm)) ]
}

check_target() {
  local kind="$1" name="$2" bcov="$3" btot="$4" acov="$5" atot="$6" pm="$7"
  local status="ok" detail=""

  # A target can violate more than one rule at once — a regression that also
  # lands below the floor is two findings, and reporting only the last one
  # would hide the other from the phase handoff. Accumulate every reason.
  add_reason() {
    [ "$status" = "ok" ] || [ "$status" = "improved" ] && status="$1" || status="$status+$1"
    [ -z "$detail" ] && detail="$2" || detail="$detail; $2"
    fail=1
  }

  if ! check_floor "$acov" "$atot" "$pm"; then
    add_reason "below_floor" "$acov/$atot is below the $((pm/10)).$((pm%10))% floor"
  fi

  if [ "$sub" = "phase" ] || [ "$sub" = "baseline" ]; then
    if [ "$btot" -gt 0 ] || [ "$bcov" -gt 0 ]; then
      # Exact fraction comparison by cross-multiplication; no rounding.
      if [ $((acov * btot)) -lt $((bcov * atot)) ]; then
        add_reason "fraction_regressed" "$bcov/$btot -> $acov/$atot"
      fi
      if [ $((atot - acov)) -gt $((btot - bcov)) ]; then
        add_reason "uncovered_increased" "uncovered $((btot-bcov)) -> $((atot-acov))"
      fi
      if [ $((acov * btot)) -gt $((bcov * atot)) ]; then
        improved=1
        [ "$status" = "ok" ] && status="improved"
      fi
    fi
  fi

  emit_row "$kind" "$name" "$acov" "$atot" "$status" "$detail"
}

read_baseline() {
  # read_baseline KIND TARGET -> "covered total", or "0 0" when absent.
  awk -v kind="$1" -v target="$2" '
    BEGIN { RS = "{"; FS = "\n" }
    $0 ~ "\"kind\"[[:space:]]*:[[:space:]]*\"" kind "\"" && $0 ~ "\"target\"[[:space:]]*:[[:space:]]*\"" target "\"" {
      c = $0; t = $0
      sub(/.*"covered"[[:space:]]*:[[:space:]]*/, "", c); sub(/[^0-9].*/, "", c)
      sub(/.*"total"[[:space:]]*:[[:space:]]*/, "", t); sub(/[^0-9].*/, "", t)
      printf "%d %d", c + 0, t + 0
      found = 1
      exit
    }
    END { if (!found) printf "0 0" }
  ' "$baseline_json"
}

for pkg in "${go_pkgs[@]}"; do
  n="$(profile_name "$pkg")"
  ac="$(go_counts "$after/$n.cover")" || { fail=1; emit_row go "$pkg" 0 0 unreadable "after profile unreadable"; continue; }
  read -r acov atot <<<"$ac"
  bcov=0; btot=0
  if [ "$sub" = "phase" ]; then
    bc="$(go_counts "$before/$n.cover")" || { fail=1; emit_row go "$pkg" "$acov" "$atot" unreadable "before profile unreadable"; continue; }
    read -r bcov btot <<<"$bc"
  elif [ "$sub" = "baseline" ]; then
    bb="$(read_baseline go "$pkg")"; read -r bcov btot <<<"$bb"
  fi
  pm="$min_pm"
  case "$pkg" in */provider/opencode|*/provider/opencode/) pm="$oc_pm" ;; esac
  check_target go "$pkg" "$bcov" "$btot" "$acov" "$atot" "$pm"
done

if [ -n "$dart_root" ] || [ "${#dart_files[@]}" -gt 0 ] || [ "${#new_dart_files[@]}" -gt 0 ]; then
  alcov="$after/flutter.lcov"
  agg="$(lcov_counts "$alcov" '*')" || { fail=1; emit_row dart "<application>" 0 0 unreadable "after lcov unreadable"; agg="0 0"; }
  read -r aacov aatot <<<"$agg"
  bacov=0; batot=0
  if [ "$sub" = "phase" ]; then
    bagg="$(lcov_counts "$before/flutter.lcov" '*')" || bagg="0 0"
    read -r bacov batot <<<"$bagg"
  elif [ "$sub" = "baseline" ]; then
    bb="$(read_baseline dart '<application>')"; read -r bacov batot <<<"$bb"
  fi
  check_target dart "<application>" "$bacov" "$batot" "$aacov" "$aatot" "$min_pm"

  for f in "${dart_files[@]}"; do
    fc="$(lcov_counts "$alcov" "$f")" || { fail=1; emit_row dart "$f" 0 0 unreadable "after lcov unreadable"; continue; }
    read -r acov atot <<<"$fc"
    if [ "$atot" -eq 0 ]; then
      emit_row dart "$f" 0 0 not_in_lcov "requested file absent from LCOV"
      fail=1
      continue
    fi
    bcov=0; btot=0
    if [ "$sub" = "phase" ]; then
      bf="$(lcov_counts "$before/flutter.lcov" "$f")" || bf="0 0"
      read -r bcov btot <<<"$bf"
    elif [ "$sub" = "baseline" ]; then
      bb="$(read_baseline dart "$f")"; read -r bcov btot <<<"$bb"
    fi
    check_target dart "$f" "$bcov" "$btot" "$acov" "$atot" "$min_pm"
  done

  for f in "${new_dart_files[@]}"; do
    fc="$(lcov_counts "$alcov" "$f")" || { fail=1; emit_row dart_new "$f" 0 0 unreadable "after lcov unreadable"; continue; }
    read -r acov atot <<<"$fc"
    if [ "$atot" -eq 0 ]; then
      emit_row dart_new "$f" 0 0 not_in_lcov "requested file absent from LCOV"
      fail=1
      continue
    fi
    check_target dart_new "$f" 0 0 "$acov" "$atot" "$nd_pm"
  done
fi

if [ "$sub" = "phase" ] && [ "$improved" -eq 0 ]; then
  fail=1
  improved_note="no executable target strictly improved"
else
  improved_note=""
fi

printf '{\n  "subcommand": "%s",\n  "minimum": "%s",\n  "opencode_floor": "%s",\n  "new_dart_floor": "%s",\n  "improved": %s,\n  "note": "%s",\n  "targets": [%s\n  ],\n  "result": "%s"\n}\n' \
  "$sub" "$minimum" "$opencode_floor" "$new_dart_floor" \
  "$([ "$improved" -eq 1 ] && printf true || printf false)" \
  "${improved_note:-}" "$json_rows" \
  "$([ "$fail" -eq 0 ] && printf pass || printf fail)"

exit "$fail"
