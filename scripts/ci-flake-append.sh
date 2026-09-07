#!/usr/bin/env bash
# Append flake row files into the committed ci-flakes.tsv ledger (MADR 0143).
#
# Deduplicates by (run_id, job_name): a re-download of the same artifact must
# not grow the ledger. Rows that are malformed (≠7 tab-separated fields, or
# empty failing_test) are skipped with a warning rather than poisoning the file.
#
# Usage:
#   scripts/ci-flake-append.sh --ledger PATH --rows-dir PATH
#
# Exit codes: 0 success (including "nothing new"); 2 bad arguments / IO error.
# Prints "appended=N skipped=M" on stdout for the workflow step summary.
set -euo pipefail

ledger=""; rows_dir=""

die() { printf 'ci-flake-append: %s\n' "$1" >&2; exit 2; }

while [ "$#" -gt 0 ]; do
  case "$1" in
  --ledger)   [ "$#" -ge 2 ] || die "--ledger needs a path"; ledger="$2"; shift 2 ;;
  --rows-dir) [ "$#" -ge 2 ] || die "--rows-dir needs a path"; rows_dir="$2"; shift 2 ;;
  --*)        die "unknown flag: $1" ;;
  *)          die "unexpected operand: $1" ;;
  esac
done

[ -n "$ledger" ]   || die "need --ledger"
[ -n "$rows_dir" ] || die "need --rows-dir"
[ -d "$rows_dir" ] || die "rows-dir is not a directory: $rows_dir"

header=$'run_id\tjob_name\tcommit_sha\tfirst_attempt_result\tretry_result\tfailing_test\ttimestamp'

if [ ! -f "$ledger" ]; then
  printf '%s\n' "$header" > "$ledger"
fi

# Ensure header is present even if someone truncated the file.
first="$(head -n 1 "$ledger" || true)"
if [ "$first" != "$header" ]; then
  die "ledger header mismatch (refuse to append): got [$first]"
fi

# Build a set of existing run_id|job_name keys.
existing="$(mktemp)"
trap 'rm -f "$existing"' EXIT
awk -F'\t' 'NR>1 && NF>=2 { print $1 "\t" $2 }' "$ledger" > "$existing"

appended=0
skipped=0

# Sort for stable commit diffs when multiple artifacts land together.
# Nullglob via find rather than a bare glob so an empty dir is fine.
while IFS= read -r -d '' f; do
  while IFS= read -r line || [ -n "$line" ]; do
    [ -n "$line" ] || continue
    # Skip accidental headers in row files.
    case "$line" in
    run_id$'\t'*) skipped=$((skipped + 1)); continue ;;
    esac
    nf="$(awk -F'\t' '{print NF}' <<<"$line")"
    failing="$(awk -F'\t' '{print $6}' <<<"$line")"
    if [ "$nf" != "7" ] || [ -z "$failing" ]; then
      printf 'ci-flake-append: skip malformed row in %s: %s\n' "$f" "$line" >&2
      skipped=$((skipped + 1))
      continue
    fi
    key="$(awk -F'\t' '{print $1 "\t" $2}' <<<"$line")"
    if grep -Fxq -- "$key" "$existing"; then
      skipped=$((skipped + 1))
      continue
    fi
    printf '%s\n' "$line" >> "$ledger"
    printf '%s\n' "$key" >> "$existing"
    appended=$((appended + 1))
  done < "$f"
done < <(find "$rows_dir" -type f \( -name '*.tsv' -o -name 'row.tsv' -o -name '*.txt' \) -print0 | sort -z)

printf 'appended=%d skipped=%d\n' "$appended" "$skipped"
