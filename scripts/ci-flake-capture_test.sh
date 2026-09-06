#!/usr/bin/env bash
# Offline tests for the MADR 0143 flake ledger scripts.
#
#   bash scripts/ci-flake-capture_test.sh
set -euo pipefail

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
CAPTURE="$HERE/ci-flake-capture.sh"
EMIT="$HERE/ci-flake-emit.sh"
APPEND="$HERE/ci-flake-append.sh"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; [ $# -gt 1 ] && printf '       %s\n' "$2"; }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "want [$3] got [$2]"; fi; }

printf '\n1. capture: extracts first --- FAIL: name\n'
cat > "$WORK/fail.log" <<'LOG'
=== RUN   TestAlpha
--- PASS: TestAlpha (0.00s)
=== RUN   TestBeta
--- FAIL: TestBeta (0.01s)
    beta_test.go:10: boom
=== RUN   TestGamma
--- FAIL: TestGamma (0.00s)
FAIL
LOG
bash "$CAPTURE" --log "$WORK/fail.log" --out "$WORK/ft.txt" --step Test
check "first FAIL wins" "$(cat "$WORK/ft.txt")" "TestBeta"

printf '\n2. capture: step-name fallback when no FAIL line\n'
cat > "$WORK/build.log" <<'LOG'
# github.com/maccavelli/magic-cli-remote/internal/foo
internal/foo/bar.go:1:2: syntax error: unexpected newline
FAIL	github.com/maccavelli/magic-cli-remote/internal/foo [build failed]
FAIL
LOG
bash "$CAPTURE" --log "$WORK/build.log" --out "$WORK/ft2.txt" --step Test
check "build failure -> step name" "$(cat "$WORK/ft2.txt")" "Test"

printf '\n3. capture: missing log -> step name\n'
bash "$CAPTURE" --log "$WORK/no-such.log" --out "$WORK/ft3.txt" --step Vet
check "missing log -> step" "$(cat "$WORK/ft3.txt")" "Vet"

printf '\n4. emit: pass-on-retry row is well-formed\n'
export FLAKE_RUN_ID=33997943225
export FLAKE_JOB_NAME='Go (windows/amd64)'
export FLAKE_COMMIT_SHA=deadbeefcafebabe
export FLAKE_EXIT_CODE=0
export FLAKE_FAILING_TEST_FILE="$WORK/ft.txt"
export FLAKE_ROW_OUT="$WORK/row-pass.tsv"
export FLAKE_STEP=Test
export FLAKE_TIMESTAMP=2026-09-05T23:00:00Z
export GITHUB_STEP_SUMMARY="$WORK/summary.md"
bash "$EMIT"
row="$(cat "$WORK/row-pass.tsv")"
nf="$(awk -F'\t' '{print NF}' <<<"$row")"
check "7 fields" "$nf" "7"
check "run_id" "$(awk -F'\t' '{print $1}' <<<"$row")" "33997943225"
check "job_name" "$(awk -F'\t' '{print $2}' <<<"$row")" "Go (windows/amd64)"
check "first_attempt" "$(awk -F'\t' '{print $4}' <<<"$row")" "fail"
check "retry_result pass" "$(awk -F'\t' '{print $5}' <<<"$row")" "pass"
check "failing_test" "$(awk -F'\t' '{print $6}' <<<"$row")" "TestBeta"
check "summary mentions TestBeta" "$(grep -c TestBeta "$WORK/summary.md" || true)" "1"

printf '\n5. emit: fail-on-retry + missing capture file uses step\n'
unset FLAKE_FAILING_TEST_FILE
export FLAKE_EXIT_CODE=1
export FLAKE_ROW_OUT="$WORK/row-fail.tsv"
export FLAKE_STEP=Test
: > "$WORK/summary.md"
bash "$EMIT"
row="$(cat "$WORK/row-fail.tsv")"
check "retry_result fail" "$(awk -F'\t' '{print $5}' <<<"$row")" "fail"
check "fallback failing_test" "$(awk -F'\t' '{print $6}' <<<"$row")" "Test"

printf '\n6. append: dedupe + malformed skip\n'
printf '%s\n' $'run_id\tjob_name\tcommit_sha\tfirst_attempt_result\tretry_result\tfailing_test\ttimestamp' \
  > "$WORK/ledger.tsv"
mkdir -p "$WORK/rows"
cp "$WORK/row-pass.tsv" "$WORK/rows/a.tsv"
# duplicate
cp "$WORK/row-pass.tsv" "$WORK/rows/a-dup.tsv"
# malformed (only 3 fields)
printf 'x\ty\tz\n' > "$WORK/rows/bad.tsv"
# second distinct row
export FLAKE_RUN_ID=33998267738
export FLAKE_JOB_NAME='Go (linux/arm64)'
export FLAKE_EXIT_CODE=1
export FLAKE_FAILING_TEST_FILE="$WORK/ft2.txt"
export FLAKE_ROW_OUT="$WORK/rows/b.tsv"
export FLAKE_STEP=Test
: > "$WORK/summary.md"
bash "$EMIT"
out="$(bash "$APPEND" --ledger "$WORK/ledger.tsv" --rows-dir "$WORK/rows")"
check "append report" "$out" "appended=2 skipped=2"
# header + 2 data rows
lines="$(wc -l < "$WORK/ledger.tsv" | tr -d ' ')"
check "ledger line count" "$lines" "3"
# plan verifier: no malformed rows
malformed="$(awk -F'\t' 'NR>1 && (NF!=7 || $6=="") {print}' "$WORK/ledger.tsv" | wc -l | tr -d ' ')"
check "no malformed data rows" "$malformed" "0"

printf '\n7. append: refuses wrong header\n'
printf 'not a header\n' > "$WORK/bad-ledger.tsv"
if bash "$APPEND" --ledger "$WORK/bad-ledger.tsv" --rows-dir "$WORK/rows" >/dev/null 2>"$WORK/err"; then
  bad "wrong header should fail"
else
  ok "wrong header rejected"
fi

printf '\nResults: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
