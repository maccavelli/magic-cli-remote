#!/usr/bin/env bash
# Auto-format Go at write time: PostToolUse hook on Edit|Write.
#
# The Go twin of post-edit-dart.sh, and the same rationale: pre-add-go.sh only
# fires when the *agent* stages the files, so a session that ends with
# unformatted Go in the working tree leaves a manual commit unprotected — and
# CI's Gofmt step fails the Go job on one drifted file. Formatting the file
# the moment the agent writes it means the tree never holds unformatted Go,
# no matter who commits it later.
#
# gofmt only, deliberately: it mirrors CI's gate (`gofmt -l cmd internal`).
# The heavier lint/vuln checks stay in pre-add-go.sh where blocking is the
# point. Never blocks: a formatter failure (syntax error mid-refactor,
# missing toolchain) must not turn an Edit into an error. Exit 0 always.
set -uo pipefail

payload="$(cat)"

# Claude Code uses tool_input (snake_case); Grok uses toolInput (camelCase).
file="$(python3 -c '
import json, sys
try:
    data = json.loads(sys.argv[1])
except Exception:
    sys.exit(0)
ti = data.get("tool_input") or data.get("toolInput") or {}
if isinstance(ti, dict):
    print(ti.get("file_path") or ti.get("filePath") or "")
' "$payload")"

case "$file" in
  *.go) ;;
  *) exit 0 ;;
esac
[ -f "$file" ] || exit 0
# No Go toolchain here is not a failure: a Flutter-only checkout must stay usable.
command -v gofmt >/dev/null 2>&1 || exit 0

before="$(shasum "$file" 2>/dev/null)"
gofmt -w "$file" >/dev/null 2>&1 || exit 0
after="$(shasum "$file" 2>/dev/null)"

if [ "$before" != "$after" ]; then
  # Tell the model the file changed under it, so the next Edit re-reads
  # instead of failing on a stale old_string.
  python3 -c '
import json, sys
print(json.dumps({"hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "post-edit-go: gofmt reformatted %s on disk; re-read it before further edits." % sys.argv[1]}}))
' "$file"
fi
exit 0
