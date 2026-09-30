#!/usr/bin/env bash
# govulncheck gate (MADR/PLAN 0174 D3, D4): no dependency with a known advisory.
#
# govulncheck reports findings at three levels: code this module calls ("Symbol
# Results"), packages it imports ("Package Results") and modules it requires
# ("Module Results"). Its "No vulnerabilities found" line speaks for the first level
# only, so a gate that trusts it misses the other two. This one fails on:
#   - any called or imported finding, allowlisted or not;
#   - any required-module finding not listed in scripts/vulncheck-allow.txt.
# An allowlisted ID is accepted only at the module level: if it ever reaches the
# imported or called level, the reason it was allowed no longer holds.
#
# Usage: scripts/vulncheck.sh        (make vulncheck; called by scripts/go-precheck.sh)
# Exit:  0 clear · 1 a finding · 2 govulncheck missing or its output unrecognised.
# Env:
#   GO_VULNCHECK_INPUT=<file>   read saved `govulncheck -show verbose` output instead of
#                               running it (scripts/vulncheck_test.sh)
#   GO_VULNCHECK_ALLOW=<file>   allowlist to use (default scripts/vulncheck-allow.txt)
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT" || exit 2
allow_file="${GO_VULNCHECK_ALLOW:-scripts/vulncheck-allow.txt}"

if [ -n "${GO_VULNCHECK_INPUT:-}" ]; then
  out="$(cat "$GO_VULNCHECK_INPUT")" || exit 2
  rc=0
else
  if ! command -v govulncheck >/dev/null 2>&1; then
    echo "vulncheck: govulncheck not found in PATH." >&2
    echo "  install: go install golang.org/x/vuln/cmd/govulncheck@v1.7.0" >&2
    exit 2
  fi
  out="$(govulncheck -show verbose ./... 2>&1)"
  rc=$?
fi

# Exit 3 is govulncheck's "called vulnerabilities found"; the parse below reports them.
# Anything else non-zero is either an unreachable database, which is not a finding and
# must not make offline work impossible, or a real failure.
if [ "$rc" -ne 0 ] && [ "$rc" -ne 3 ]; then
  if printf '%s' "$out" | grep -qiE 'no such host|connection refused|timeout|dial tcp|proxy'; then
    echo "vulncheck: could not reach the vulnerability database; skipped." >&2
    exit 0
  fi
  echo "vulncheck: govulncheck exited $rc:" >&2
  printf '%s\n' "$out" | tail -30 | sed 's/^/  /' >&2
  exit 1
fi

for section in 'Symbol Results' 'Package Results' 'Module Results'; do
  if ! printf '%s\n' "$out" | grep -q "^=== $section ===\$"; then
    echo "vulncheck: govulncheck output has no \"=== $section ===\" section; refusing to pass blind." >&2
    exit 2
  fi
done

# "<level> <ID>" per finding: level is called, imported or required.
findings="$(printf '%s\n' "$out" | awk '
  /^=== Symbol Results ===$/  { level = "called" }
  /^=== Package Results ===$/ { level = "imported" }
  /^=== Module Results ===$/  { level = "required" }
  /^Vulnerability #[0-9]+: / && level != "" { id = $3; print level, id }
')"

allowed=""
if [ -f "$allow_file" ]; then
  allowed="$(grep -vE '^[[:space:]]*(#|$)' "$allow_file" | cut -f1 | tr -d '[:space:]\r' | sort -u | tr '\n' ' ')"
fi
is_allowed() { case " $allowed " in *" $1 "*) return 0 ;; esac; return 1; }

failed=0
accepted=""
while read -r level id; do
  [ -n "${id:-}" ] || continue
  case "$level" in
  called | imported)
    if is_allowed "$id"; then
      echo "vulncheck: $id is $level by this module. It is allowlisted only as a required module that is not in the build, so that no longer holds." >&2
    else
      echo "vulncheck: $id is $level by this module." >&2
    fi
    failed=1
    ;;
  required)
    if is_allowed "$id"; then
      accepted="$accepted $id"
    else
      echo "vulncheck: $id affects a required module and is not in $allow_file." >&2
      failed=1
    fi
    ;;
  esac
done <<<"$findings"

for id in $allowed; do
  if ! printf '%s\n' "$findings" | grep -q " $id\$"; then
    echo "vulncheck: $id is allowlisted but no longer reported; remove it from $allow_file." >&2
  fi
done

if [ "$failed" -ne 0 ]; then
  echo "vulncheck: govulncheck -show verbose (tail):" >&2
  printf '%s\n' "$out" | tail -30 | sed 's/^/  /' >&2
  exit 1
fi
if [ -n "$accepted" ]; then
  echo "vulncheck: clear; allowlisted required-module findings:$accepted"
else
  echo "vulncheck: clear."
fi
exit 0
