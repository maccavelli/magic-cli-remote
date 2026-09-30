#!/usr/bin/env bash
# Offline tests for the MADR 0174 govulncheck gate (scripts/vulncheck.sh). Fixtures
# follow `govulncheck -show verbose` output (v1.7.0).
#
#   bash scripts/vulncheck_test.sh
set -euo pipefail

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
GATE="$HERE/vulncheck.sh"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; [ $# -gt 1 ] && printf '       %s\n' "$2"; }

# vuln <ID>: one finding block as govulncheck prints it.
vuln() {
  printf 'Vulnerability #1: %s\n    A fixture advisory\n  More info: https://pkg.go.dev/vuln/%s\n' "$1" "$1"
  printf '  Module: golang.org/x/crypto\n    Found in: golang.org/x/crypto@v0.57.0\n    Fixed in: N/A\n\n'
}
# output <called-ID|-> <imported-ID|-> <required-ID|->: a whole verbose report.
output() {
  printf 'Fetching vulnerabilities from the database...\n\n=== Symbol Results ===\n\n'
  if [ "$1" = - ]; then printf 'No vulnerabilities found.\n\n'; else vuln "$1"; fi
  printf '=== Package Results ===\n\n'
  if [ "$2" = - ]; then printf 'No other vulnerabilities found.\n\n'; else vuln "$2"; fi
  printf '=== Module Results ===\n\n'
  if [ "$3" = - ]; then printf 'No other vulnerabilities found.\n\n'; else vuln "$3"; fi
  printf 'Your code is affected by 0 vulnerabilities.\n'
}

printf 'GO-2026-5932\tgolang.org/x/crypto\t2026-09-29\tfixture\n' > "$WORK/allow.txt"
: > "$WORK/empty-allow.txt"

# run <name> <want-exit> <want-stderr-regex> <allowlist> <called> <imported> <required>
run() {
  local name=$1 want=$2 pattern=$3 allow=$4
  output "$5" "$6" "$7" > "$WORK/in.txt"
  set +e
  GO_VULNCHECK_INPUT="$WORK/in.txt" GO_VULNCHECK_ALLOW="$allow" bash "$GATE" > "$WORK/out.txt" 2> "$WORK/err.txt"
  local got=$?
  set -e
  if [ "$got" -ne "$want" ]; then
    bad "$name" "want exit $want got $got: $(head -2 "$WORK/err.txt")"
  elif [ -n "$pattern" ] && ! grep -qE "$pattern" "$WORK/err.txt" "$WORK/out.txt"; then
    bad "$name" "no line matching /$pattern/"
  else
    ok "$name"
  fi
}

printf '\n1. an allowlisted required-module finding passes\n'
run "allowlisted, required" 0 'allowlisted required-module findings: GO-2026-5932' "$WORK/allow.txt" - - GO-2026-5932
run "nothing reported" 0 'vulncheck: clear\.$' "$WORK/allow.txt" - - -

printf '\n2. a required-module finding that is not allowlisted fails\n'
run "not allowlisted, required" 1 'GO-2026-5932 affects a required module and is not in' "$WORK/empty-allow.txt" - - GO-2026-5932
run "another ID, required" 1 'GO-2026-6354 affects a required module' "$WORK/allow.txt" - - GO-2026-6354

printf '\n3. an allowlisted ID fails once it is imported or called\n'
run "allowlisted, imported" 1 'GO-2026-5932 is imported by this module\. It is allowlisted only' "$WORK/allow.txt" - GO-2026-5932 -
run "allowlisted, called" 1 'GO-2026-5932 is called by this module\. It is allowlisted only' "$WORK/allow.txt" GO-2026-5932 - -

printf '\n4. output without the three sections is refused, not passed\n'
printf 'No vulnerabilities found.\n' > "$WORK/in.txt"
set +e
GO_VULNCHECK_INPUT="$WORK/in.txt" GO_VULNCHECK_ALLOW="$WORK/allow.txt" bash "$GATE" > /dev/null 2> "$WORK/err.txt"
got=$?
set -e
if [ "$got" -eq 2 ] && grep -q 'refusing to pass blind' "$WORK/err.txt"; then ok "unrecognised output"; else bad "unrecognised output" "exit $got"; fi

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
