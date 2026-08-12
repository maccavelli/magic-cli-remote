#!/usr/bin/env bash
# Auto-format Dart at write time: PostToolUse hook on Edit|Write.
#
# Why this exists alongside pre-add-dart.sh: that gate only fires when the
# *agent* stages the files. Commit eebb1e7 went red in CI because a session
# ended with unformatted Dart sitting in the working tree, and the commit was
# then made from a regular terminal, where agent hooks never run. Formatting
# the file the moment the agent writes it means the tree never holds
# unformatted Dart, no matter who commits it later.
#
# Never blocks: a formatter failure (syntax error mid-refactor, missing
# toolchain) must not turn an Edit into an error. Exit 0 always.
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
  *.dart) ;;
  *) exit 0 ;;
esac
[ -f "$file" ] || exit 0
# No Dart toolchain here is not a failure: a Go-only checkout must stay usable.
command -v dart >/dev/null 2>&1 || exit 0

before="$(shasum "$file" 2>/dev/null)"
dart format "$file" >/dev/null 2>&1 || exit 0
after="$(shasum "$file" 2>/dev/null)"

if [ "$before" != "$after" ]; then
  # Tell the model the file changed under it, so the next Edit re-reads
  # instead of failing on a stale old_string.
  python3 -c '
import json, sys
print(json.dumps({"hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "post-edit-dart: dart format reformatted %s on disk; re-read it before further edits." % sys.argv[1]}}))
' "$file"
fi
exit 0
