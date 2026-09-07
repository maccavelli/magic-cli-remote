#!/usr/bin/env bash
# Compose one MADR 0143 ledger row after nick-fields/retry reports total_attempts > 1.
#
# Environment (all required unless noted):
#   FLAKE_RUN_ID              github.run_id
#   FLAKE_JOB_NAME            e.g. "Go (windows/amd64)"
#   FLAKE_COMMIT_SHA          github.sha
#   FLAKE_EXIT_CODE           steps.test.outputs.exit_code (final attempt)
#   FLAKE_FAILING_TEST_FILE   path written by ci-flake-capture.sh (optional if
#                             FLAKE_STEP is set — falls back to step name)
#   FLAKE_ROW_OUT             destination .tsv path (one row, no header)
#   FLAKE_STEP                step-name fallback (default: Test)
#   FLAKE_TIMESTAMP           optional ISO-8601 UTC; default: now
#
# first_attempt_result is always "fail": this script only runs when a retry
# happened, which means attempt 1 failed. retry_result is pass/fail from the
# final exit code.
#
# Also writes a step-summary block and a notice annotation so a retry is
# visible beyond warning_on_retry.
set -euo pipefail

die() { printf 'ci-flake-emit: %s\n' "$1" >&2; exit 2; }

run_id="${FLAKE_RUN_ID:-}"
job_name="${FLAKE_JOB_NAME:-}"
commit_sha="${FLAKE_COMMIT_SHA:-}"
exit_code="${FLAKE_EXIT_CODE:-}"
ft_file="${FLAKE_FAILING_TEST_FILE:-}"
row_out="${FLAKE_ROW_OUT:-}"
step="${FLAKE_STEP:-Test}"
ts="${FLAKE_TIMESTAMP:-}"

[ -n "$run_id" ]     || die "FLAKE_RUN_ID required"
[ -n "$job_name" ]   || die "FLAKE_JOB_NAME required"
[ -n "$commit_sha" ] || die "FLAKE_COMMIT_SHA required"
[ -n "$exit_code" ]  || die "FLAKE_EXIT_CODE required"
[ -n "$row_out" ]    || die "FLAKE_ROW_OUT required"

failing=""
if [ -n "$ft_file" ] && [ -f "$ft_file" ]; then
  failing="$(tr -d '\r\n' < "$ft_file")"
fi
[ -n "$failing" ] || failing="$step"
[ -n "$failing" ] || die "refusing empty failing_test"

if [ "$exit_code" = "0" ]; then
  retry_result="pass"
else
  retry_result="fail"
fi
first_attempt_result="fail"

if [ -z "$ts" ]; then
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

# Tabs only; no newlines inside fields. job_name may contain spaces/parens.
# Strip any tabs/newlines that somehow landed in inputs.
sanitize() { printf '%s' "$1" | tr -d '\t\r\n'; }

run_id="$(sanitize "$run_id")"
job_name="$(sanitize "$job_name")"
commit_sha="$(sanitize "$commit_sha")"
first_attempt_result="$(sanitize "$first_attempt_result")"
retry_result="$(sanitize "$retry_result")"
failing="$(sanitize "$failing")"
ts="$(sanitize "$ts")"

mkdir -p "$(dirname -- "$row_out")"
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
  "$run_id" "$job_name" "$commit_sha" \
  "$first_attempt_result" "$retry_result" "$failing" "$ts" \
  > "$row_out"

# Visibility beyond warning_on_retry (MADR Confirmation #4 already met by the
# action; this names the test in the job summary and Actions annotations).
{
  echo "### CI flake ledger (MADR 0143)"
  echo ""
  echo "| field | value |"
  echo "| --- | --- |"
  echo "| run_id | \`$run_id\` |"
  echo "| job | \`$job_name\` |"
  echo "| commit | \`$commit_sha\` |"
  echo "| first_attempt | \`$first_attempt_result\` |"
  echo "| retry | \`$retry_result\` |"
  echo "| failing_test | \`$failing\` |"
  echo "| timestamp | \`$ts\` |"
} >> "${GITHUB_STEP_SUMMARY:-/dev/null}"

echo "::notice title=CI retry recorded::job=$job_name failing_test=$failing retry=$retry_result"
printf 'ci-flake-emit: wrote %s\n' "$row_out"
