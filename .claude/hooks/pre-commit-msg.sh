#!/usr/bin/env bash
# Agent gate for the git commit message rule in AGENTS.md:
# Do NOT pass a commit message (-m / --message / -F). The global prepare-commit-msg
# hook populates the message automatically.

set -uo pipefail

payload="$(cat)"

read -r -d '' pyprog <<'PY'
import json, re, sys

payload = sys.argv[1]
try:
    data = json.loads(payload)
except Exception:
    sys.exit(0)

# Claude Code uses tool_input (snake_case); Grok uses toolInput (camelCase).
tool_input = data.get("tool_input") or data.get("toolInput") or {}
if not isinstance(tool_input, dict):
    tool_input = {}
cmd = tool_input.get("command") or ""

# Match git commit with -m, -M, --message, or -F
if re.search(r"\bgit\s+(?:-[^\s]+\s+)*commit\b.*?(?:-m|-M|--message|-F)\b", cmd):
    print("MATCH")
PY

result="$(python3 -c "$pyprog" "$payload")"
if [ "$result" = "MATCH" ]; then
  reason="Blocked: Do NOT pass a commit message (-m, -M, --message, or -F) when running 'git commit' (AGENTS.md). A global git prepare-commit-msg hook populates commit messages automatically. Run 'git commit' without a message argument."
  # Grok PreToolUse honors JSON deny on stdout; Claude Code uses exit 2 + stderr.
  printf '%s\n' "{\"decision\":\"deny\",\"reason\":$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$reason")}"
  echo "$reason" >&2
  exit 2
fi
exit 0
